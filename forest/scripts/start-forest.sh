#!/bin/bash

no="$1"

forest_data_dir="FOREST_${no}_DATA_DIR"
export FOREST_DATA_DIR="${!forest_data_dir}"

forest_ip="FOREST_${no}_IP"
export FOREST_IP="${!forest_ip}"

forest_rpc_port="FOREST_${no}_RPC_PORT"
export FOREST_RPC_PORT="${!forest_rpc_port}"

forest_p2p_port="FOREST_${no}_P2P_PORT"
export FOREST_P2P_PORT="${!forest_p2p_port}"

forest_healthz_rpc_port="FOREST_${no}_HEALTHZ_RPC_PORT"
export FOREST_HEALTHZ_RPC_PORT="${!forest_healthz_rpc_port}"

#TODO: This is probably going to need to be dynamic
export FOREST_TARGET_PEER_COUNT=$(($NUM_LOTUS_CLIENTS + $NUM_FOREST_CLIENTS - 1))

export FOREST_F3_BOOTSTRAP_EPOCH=10
export FOREST_F3_FINALITY=5
export FOREST_CHAIN_INDEXER_ENABLED=true
export FOREST_BLOCK_DELAY_SECS=4
export FOREST_PROPAGATION_DELAY_SECS=1

while true; do
    echo "forest${no}: Fetching drand chain info from ${DRAND0_IP}..."
    response=$(curl -s --fail "http://${DRAND0_IP}/info" 2>&1)
    
    if [ $? -eq 0 ] && echo "$response" | jq -e '.public_key?' >/dev/null 2>&1; then

        # forest chain info needs to be in this format?
        formatted_json=$(jq --arg server "http://${DRAND0_IP}" '{ servers: [$server], chain_info: { public_key: .public_key, period: .period, genesis_time: .genesis_time, hash: .hash, groupHash: .groupHash }, network_type: "Quicknet" }' <<<"$response")
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

sed "s|\${FOREST_DATA_DIR}|$FOREST_DATA_DIR|g; s|\${FOREST_TARGET_PEER_COUNT}|$FOREST_TARGET_PEER_COUNT|g" /forest/forest_config.toml.tpl > ${FOREST_DATA_DIR}/forest_config.toml
echo "name = \"${NETWORK_NAME}\"" >> "${FOREST_DATA_DIR}/forest_config.toml"

# Perform basic initialization of the Forest node, including generating the admin token.
forest --genesis "${SHARED_CONFIGS}/devgen.car" \
       --config "${FOREST_DATA_DIR}/forest_config.toml" \
       --save-token "${FOREST_DATA_DIR}/jwt" \
       --no-healthcheck \
       --skip-load-actors \
       --exit-after-init

forest --genesis "${SHARED_CONFIGS}/devgen.car" \
       --config "${FOREST_DATA_DIR}/forest_config.toml" \
       --rpc-address "${FOREST_IP}:${FOREST_RPC_PORT}" \
       --p2p-listen-address "/ip4/${FOREST_IP}/tcp/${FOREST_P2P_PORT}" \
       --healthcheck-address "${FOREST_IP}:${FOREST_HEALTHZ_RPC_PORT}" \
       --skip-load-actors &

# Admin token is required for connection commands and wallet management.
export TOKEN=$(cat "${FOREST_DATA_DIR}/jwt")
export FULLNODE_API_INFO="$TOKEN:/ip4/${FOREST_IP}/tcp/${FOREST_RPC_PORT}/http"
echo "FULLNODE_API_INFO: $FULLNODE_API_INFO"

# forest node API needs to be up
forest-cli wait-api
echo "forest: collecting network info…"

forest-cli net listen | head -n1 > "${FOREST_DATA_DIR}/forest${no}-ipv4addr"

# connecting to peers
retries=10
connect_with_retries() {
  local addr_file="$1"

  for (( i=1; i<=retries; i++ )); do
    echo "attempt $i..."

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
