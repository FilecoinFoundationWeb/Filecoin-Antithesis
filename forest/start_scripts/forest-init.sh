#!/bin/bash
set -euo pipefail

# Function to check if DRAND server is healthy
check_drand_server() {
  local response_code
  response_code=$(curl -s -o /dev/null -w "%{http_code}" "$DRAND_SERVER/info")
  
  if [ "$response_code" != "200" ]; then
    echo "Error: DRAND server is not ready (HTTP $response_code)"
    return 1
  fi
  return 0
}

sleep 10

DRAND_SERVER="http://10.20.20.21"

echo "Waiting for DRAND server to be ready..."
while ! check_drand_server; do
  sleep 5
done
echo "DRAND server is ready"
  
json=$(curl -s "$DRAND_SERVER/info")
formatted_json=$(jq --arg server "$DRAND_SERVER" '{ servers: [$server], chain_info: { public_key: .public_key, period: .period, genesis_time: .genesis_time, hash: .hash, groupHash: .groupHash }, network_type: "Quicknet" }' <<<"$json")
echo "formatted_json: $formatted_json"
export FOREST_DRAND_QUICKNET_CONFIG="$formatted_json"
export FOREST_F3_BOOTSTRAP_EPOCH=10
export FOREST_F3_FINALITY=5
export FOREST_CHAIN_INDEXER_ENABLED=true
export FOREST_BLOCK_DELAY_SECS=4
export FOREST_PROPAGATION_DELAY_SECS=1
NETWORK_NAME=$(jq -r '.NetworkName' "${LOTUS_1_DATA_DIR}/localnet.json")
export NETWORK_NAME=$NETWORK_NAME
forest --version
cp /forest/forest_config.toml.tpl "${FOREST_DATA_DIR}/forest_config.toml"
echo "name = \"${NETWORK_NAME}\"" >> "${FOREST_DATA_DIR}/forest_config.toml"

# Perform basic initialization of the Forest node, including generating the admin token.
forest --genesis "${LOTUS_1_DATA_DIR}/devgen.car" \
       --config "${FOREST_DATA_DIR}/forest_config.toml" \
       --save-token "${FOREST_DATA_DIR}/jwt" \
       --no-healthcheck \
       --exit-after-init

forest --genesis "${LOTUS_1_DATA_DIR}/devgen.car" \
       --config "${FOREST_DATA_DIR}/forest_config.toml" \
       --rpc-address "${FOREST_IP}:${FOREST_RPC_PORT}" \
       --p2p-listen-address "/ip4/${FOREST_IP}/tcp/${FOREST_P2P_PORT}" \
       --healthcheck-address "${FOREST_IP}:${FOREST_HEALTHZ_RPC_PORT}" &

# Admin token is required for connection commands and wallet management.
TOKEN=$(cat "${FOREST_DATA_DIR}/jwt")
export TOKEN
FULLNODE_API_INFO=$TOKEN:/ip4/${FOREST_IP}/tcp/${FOREST_RPC_PORT}/http
export FULLNODE_API_INFO

echo "FULLNODE_API_INFO: $FULLNODE_API_INFO"

# Ensure the Forest node API is up before calling other commands
forest-cli wait-api
echo "forest: collecting network info…"

forest-cli net listen | head -n1 > "${FOREST_DATA_DIR}/forest-listen-addr"
echo "forest: connecting to lotus nodes…"
forest-cli net connect "$(cat "${LOTUS_1_DATA_DIR}/lotus-1-ipv4addr")"
forest-cli net connect "$(cat "${LOTUS_2_DATA_DIR}/lotus-2-ipv4addr")"

# Ensure the Forest node is fully synced before proceeding
forest-cli sync wait
forest-cli sync status
forest-cli healthcheck healthy --healthcheck-port "${FOREST_HEALTHZ_RPC_PORT}"
echo "forest: ready."
sleep infinity
