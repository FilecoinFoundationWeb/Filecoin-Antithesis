#!/bin/bash

no="$1"

forest_data_dir="FOREST_${no}_DATA_DIR"
export FOREST_DATA_DIR="${!forest_data_dir}"
export LD_LIBRARY_PATH="/usr/local/lib:${LD_LIBRARY_PATH}"

export FOREST_RPC_PORT=$FOREST_RPC_PORT
export FOREST_P2P_PORT=$FOREST_P2P_PORT
export FOREST_HEALTHZ_RPC_PORT=$FOREST_HEALTHZ_RPC_PORT
export FOREST_TARGET_PEER_COUNT=$(($NUM_LOTUS_CLIENTS + $NUM_FOREST_CLIENTS - 1))

forest_0_f3_sidecar_rpc_endpoint="FOREST_${no}_F3_SIDECAR_RPC_ENDPOINT"
export FOREST_F3_SIDECAR_RPC_ENDPOINT="${!forest_0_f3_sidecar_rpc_endpoint}"

export FOREST_F3_BOOTSTRAP_EPOCH=5
export FOREST_F3_FINALITY=2
export FOREST_CHAIN_INDEXER_ENABLED=true
export FOREST_BLOCK_DELAY_SECS=4
export FOREST_PROPAGATION_DELAY_SECS=1

while true; do
    echo "forest${no}: Fetching drand chain info from drand0..."
    response=$(curl -s --fail "http://drand0/info" 2>&1)
    
    if [ $? -eq 0 ] && echo "$response" | jq -e '.public_key?' >/dev/null 2>&1; then

        # forest chain info needs to be in this format?
        formatted_json=$(jq --arg server "http://drand0" '{ servers: [$server], chain_info: { public_key: .public_key, period: .period, genesis_time: .genesis_time, hash: .hash, groupHash: .groupHash }, network_type: "Quicknet" }' <<<"$response")
        echo "formatted_json: $formatted_json"
        export FOREST_DRAND_QUICKNET_CONFIG="$formatted_json"
        echo "forest${no}: Drand chain info ready"
        break
    else
        sleep 2
    fi
done

NETWORK_NAME=$(jq -r '.NetworkName' "${SHARED_CONFIGS}/localnet.json")
export NETWORK_NAME=$NETWORK_NAME

forest --version

host_ip=$(getent hosts "forest${no}" | awk '{ print $1 }')

if [ ! -f "${FOREST_DATA_DIR}/jwt" ]; then
    sed "s|\${FOREST_DATA_DIR}|$FOREST_DATA_DIR|g; s|\${FOREST_TARGET_PEER_COUNT}|$FOREST_TARGET_PEER_COUNT|g" /forest/forest_config.toml.tpl > ${FOREST_DATA_DIR}/forest_config.toml
    echo "name = \"${NETWORK_NAME}\"" >> "${FOREST_DATA_DIR}/forest_config.toml"

    echo "---------------------------"
    echo "ip address: $host_ip"
    echo "---------------------------"

    # Perform basic initialization of the Forest node, including generating the admin token.
    forest --genesis "${SHARED_CONFIGS}/devgen.car" \
        --config "${FOREST_DATA_DIR}/forest_config.toml" \
        --save-token "${FOREST_DATA_DIR}/jwt" \
        --no-healthcheck \
        --exit-after-init
else
    echo "forest${no}: Node already initialized, skipping init..."
    # Still need to regenerate config in case env vars changed, but skipping exit-after-init
    sed "s|\${FOREST_DATA_DIR}|$FOREST_DATA_DIR|g; s|\${FOREST_TARGET_PEER_COUNT}|$FOREST_TARGET_PEER_COUNT|g" /forest/forest_config.toml.tpl > ${FOREST_DATA_DIR}/forest_config.toml
    echo "name = \"${NETWORK_NAME}\"" >> "${FOREST_DATA_DIR}/forest_config.toml"
fi

forest --genesis "${SHARED_CONFIGS}/devgen.car" \
       --config "${FOREST_DATA_DIR}/forest_config.toml" \
       --rpc-address "${host_ip}:${FOREST_RPC_PORT}" \
       --p2p-listen-address "/ip4/${host_ip}/tcp/${FOREST_P2P_PORT}" \
       --healthcheck-address "${host_ip}:${FOREST_HEALTHZ_RPC_PORT}" \
       --skip-load-actors &

# Admin token is required for connection commands and wallet management.
export TOKEN=$(cat "${FOREST_DATA_DIR}/jwt")
export FULLNODE_API_INFO="$TOKEN:/ip4/$host_ip/tcp/${FOREST_RPC_PORT}/http"
echo "FULLNODE_API_INFO: $FULLNODE_API_INFO"

# forest node API needs to be up
forest-cli wait-api
echo "forest${no}: collecting network info…"

# Export artifacts similar to Lotus pattern
# Store IPv4 address
forest-cli net listen | grep -v "127.0.0.1" | grep -v "::1" | head -n 1 > "${FOREST_DATA_DIR}/forest${no}-ipv4addr"

# Copy JWT with lotus-style naming for workload compatibility  
cp "${FOREST_DATA_DIR}/jwt" "${FOREST_DATA_DIR}/forest${no}-jwt"

# Export P2P ID
forest-cli net id > "${FOREST_DATA_DIR}/forest${no}-p2pid" 2>/dev/null || echo "P2P ID export skipped"

echo "forest${no}: Exported artifacts to ${FOREST_DATA_DIR}:"
ls -la "${FOREST_DATA_DIR}/forest${no}-"* 2>/dev/null || true

# Import all genesis miner pre-seal keys so F3 can sign messages
echo "forest${no}: Importing genesis miner keys..."
for PRESEAL_KEY_FILE in ${SHARED_CONFIGS}/.genesis-sector-*/pre-seal-*.key; do
    if [ -f "$PRESEAL_KEY_FILE" ]; then
        echo "Importing pre-seal key from $PRESEAL_KEY_FILE"
        forest-wallet --remote-wallet import "$PRESEAL_KEY_FILE" || true
    fi
done

# connecting to peers
connect_with_retries() {
  local retries=10
  local addr_file="$1"

  for (( j=1; j<=retries; j++ )); do
    echo "attempt $j..."

    ip=$(<"$addr_file")
    
    if forest-cli net connect "$ip"; then
      echo "successful connect!"
      return 0
    else
      sleep 2
    fi
  done

  echo "ERROR: reached $MAX_RETRIES attempts."
  return 1
}

echo "forest: connecting to lotus nodes..."
for (( i=0; i<$NUM_LOTUS_CLIENTS; i++ )); do
  lotus_data_dir="LOTUS_${i}_DATA_DIR"
  LOTUS_DATA_DIR="${!lotus_data_dir}"
  addr_file="${LOTUS_DATA_DIR}/lotus${i}-ipv4addr"

  echo "Connecting to lotus$i at $addr_file"
  connect_with_retries "$addr_file"
done

echo "forest: connecting to other forest nodes..."
for (( i=0; i<$NUM_FOREST_CLIENTS; i++ )); do
  if [[ $i -eq $no ]]; then
    continue  # skip connecting to self
  fi

  other_forest_data_dir="FOREST_${i}_DATA_DIR"
  OTHER_FOREST_DATA_DIR="${!other_forest_data_dir}"
  addr_file="${OTHER_FOREST_DATA_DIR}/forest${i}-ipv4addr"

  echo "Connecting to lotus$i at $addr_file"
  connect_with_retries "$addr_file"
done

# Ensure the Forest node is fully synced before proceeding
forest-cli sync wait
forest-cli sync status
forest-cli healthcheck healthy --healthcheck-port "${FOREST_HEALTHZ_RPC_PORT}"

echo "forest${no}: completed startup"

sleep infinity
