#!/bin/bash
# Enable strict mode to catch errors and undefined variables
set -euo pipefail

DRAND_SERVER="http://10.20.20.21"

# Fetch JSON from the Drand endpoint
json=$(curl -s "$DRAND_SERVER/info")

# Format the JSON into the required structure
formatted_json=$(jq --arg server "$DRAND_SERVER" '
{
  servers: [$server],
  chain_info: {
    public_key: .public_key,
    period: .period,
    genesis_time: .genesis_time,
    hash: .hash,
    groupHash: .groupHash
  },
  network_type: "Quicknet"
}' <<< "$json")

# Export the formatted JSON as an environment variable
export FOREST_DRAND_QUICKNET_CONFIG="$formatted_json"
echo $FOREST_DRAND_QUICKNET_CONFIG
export FOREST_F3_FINALITY=20
# Extract network name from localnet.json and set it as an environment variable
export NETWORK_NAME=$(grep -o "localnet.*" "${LOTUS_1_DATA_DIR}/localnet.json" | tr -d '",' )
forest --version
# Copy the forest configuration template and update it with the network name
cp /forest/forest_config.toml.tpl "${FOREST_DATA_DIR}/forest_config.toml"
echo "name = \"${NETWORK_NAME}\"" >> "${FOREST_DATA_DIR}/forest_config.toml"

# Load the token and set the full node API information
cat ${FOREST_DATA_DIR}/forest_config.toml
# export FULLNODE_API_INFO=$TOKEN:/ip4/10.20.20.27/tcp/${FOREST_RPC_PORT}/http
# Start the forest service with the specified configuration
 forest --genesis "${LOTUS_1_DATA_DIR}/devgen.car" \
       --config "${FOREST_DATA_DIR}/forest_config.toml" \
       --save-token "${FOREST_DATA_DIR}/jwt" \
       --rpc-address ${FOREST_IP}:${FOREST_RPC_PORT} \
       --p2p-listen-address /ip4/${FOREST_IP}/tcp/${FOREST_P2P_PORT} \
       --skip-load-actors &

sleep infinity