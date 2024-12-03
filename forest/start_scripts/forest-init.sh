#!/bin/bash
# Enable strict mode to catch errors and undefined variables
set -euo pipefail

sleep 20

# Waiting for lotus node 1 to be up
lotus_1_ready=0
while [[ ${lotus_1_ready?} -eq 0 ]]
do
    echo "forest: checking if lotus-1 is up.."
    if [[ -e "/container_ready/lotus-1" ]]
    then
        echo "forest: lotus-1 is ready!"
        echo "forest: continuing startup..."
        lotus_1_ready=1
        break
    fi
    sleep 5
done

# Fetch and save DRAND chain information
curl 10.20.20.21/info | jq -c > chain_info
export DRAND_CHAIN_INFO=chain_info
# Extract network name from localnet.json and set it as an environment variable
export NETWORK_NAME=$(grep -o "localnet.*" "${LOTUS_1_DATA_DIR}/localnet.json" | tr -d '",' )

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

touch /container_ready/forest
sleep infinity