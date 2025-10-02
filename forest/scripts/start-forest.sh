#!/bin/bash

# Function to check if DRAND server is healthy
check_drand_server() {
  local response_code
  response_code=$(curl -s -o /dev/null -w "%{http_code}" "http://${DRAND0_IP}/info")
  
  if [ "$response_code" != "200" ]; then
    echo "Error: DRAND server is not ready (HTTP $response_code)"
    return 1
  fi
  return 0
}

DRAND_SERVER="http://${DRAND0_IP}"

echo "Waiting for DRAND server to be ready..."
while ! check_drand_server; do
  sleep 5
done
echo "DRAND server is ready"

json=$(curl -s "http://${DRAND0_IP}/info")
formatted_json=$(jq --arg server "$DRAND_SERVER" '{ servers: [$server], chain_info: { public_key: .public_key, period: .period, genesis_time: .genesis_time, hash: .hash, groupHash: .groupHash }, network_type: "Quicknet" }' <<<"$json")

echo "formatted_json: $formatted_json"
export FOREST_DRAND_QUICKNET_CONFIG="$formatted_json"
export FOREST_F3_BOOTSTRAP_EPOCH=10
export FOREST_F3_FINALITY=5
export FOREST_CHAIN_INDEXER_ENABLED=true
export FOREST_BLOCK_DELAY_SECS=4
export FOREST_PROPAGATION_DELAY_SECS=1
NETWORK_NAME=$(jq -r '.NetworkName' "${SHARED_CONFIGS}/localnet.json")
export NETWORK_NAME=$NETWORK_NAME
forest --version
cp /forest/forest_config.toml.tpl "${FOREST_0_DATA_DIR}/forest_config.toml"
echo "name = \"${NETWORK_NAME}\"" >> "${FOREST_0_DATA_DIR}/forest_config.toml"

# Perform basic initialization of the Forest node, including generating the admin token.
forest --genesis "${SHARED_CONFIGS}/devgen.car" \
       --config "${FOREST_0_DATA_DIR}/forest_config.toml" \
       --save-token "${FOREST_0_DATA_DIR}/jwt" \
       --no-healthcheck \
       --skip-load-actors \
       --exit-after-init

forest --genesis "${Shared_configs}/devgen.car" \
       --config "${FOREST_0_DATA_DIR}/forest_config.toml" \
       --rpc-address "${FOREST_0_IP}:${FOREST_0_RPC_PORT}" \
       --p2p-listen-address "/ip4/${FOREST_0_IP}/tcp/${FOREST_0_P2P_PORT}" \
       --healthcheck-address "${FOREST_0_IP}:${FOREST_0_HEALTHZ_RPC_PORT}" \
       --skip-load-actors &

# Admin token is required for connection commands and wallet management.
TOKEN=$(cat "${FOREST_0_DATA_DIR}/jwt")
export TOKEN
FULLNODE_API_INFO=$TOKEN:/ip4/${FOREST_0_IP}/tcp/${FOREST_0_RPC_PORT}/http
export FULLNODE_API_INFO

echo "FULLNODE_API_INFO: $FULLNODE_API_INFO"

# Ensure the Forest node API is up before calling other commands
forest-cli wait-api
echo "forest: collecting network info…"

forest-cli net listen | head -n1 > "${FOREST_0_DATA_DIR}/forest-listen-addr"
echo "forest: connecting to lotus nodes…"
forest-cli net connect "$(cat "${LOTUS_0_DATA_DIR}/lotus0-ipv4addr")"
forest-cli net connect "$(cat "${LOTUS_1_DATA_DIR}/lotus1-ipv4addr")"

# Ensure the Forest node is fully synced before proceeding
forest-cli sync wait
forest-cli sync status
forest-cli healthcheck healthy --healthcheck-port "${FOREST_0_HEALTHZ_RPC_PORT}"
echo "forest: ready."
sleep infinity
