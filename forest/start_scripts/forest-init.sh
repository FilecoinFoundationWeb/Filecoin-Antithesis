#!/bin/bash
set -xeuo pipefail

# Function to wait for chain info with timeout
wait_for_chain_info() {
    local chain_info_file="${LOTUS_1_DATA_DIR}/chain_info"
    local timeout=300  # 5 minutes timeout
    local start_time=$(date +%s)
    
    echo "Waiting for chain info file at: $chain_info_file"
    while [ ! -f "$chain_info_file" ] || [ ! -s "$chain_info_file" ]; do
        if [ $(($(date +%s) - start_time)) -gt "$timeout" ]; then
            echo "Error: Timeout waiting for chain info file"
            return 1
        fi
        echo "Chain info file not ready, waiting..."
        sleep 5
    done
    
    # Validate JSON format
    if ! jq empty "$chain_info_file" 2>/dev/null; then
        echo "Error: Invalid JSON in chain info file"
        return 1
    fi
    
    echo "Chain info file is ready and valid"
    return 0
}

# Wait for chain info to be available
if ! wait_for_chain_info; then
    echo "Failed to get chain info within timeout"
    exit 1
fi

# Read and format chain info for Forest
json=$(cat "${LOTUS_1_DATA_DIR}/chain_info")
formatted_json=$(jq '{
    servers: ["http://10.20.20.21"],
    chain_info: {
        public_key: .public_key,
        period: .period,
        genesis_time: .genesis_time,
        hash: .hash,
        groupHash: .groupHash
    },
    network_type: "Quicknet"
}' <<<"$json")
echo "formatted_json: $formatted_json"
export FOREST_DRAND_QUICKNET_CONFIG="$formatted_json"
export FOREST_F3_BOOTSTRAP_EPOCH=10
export FOREST_F3_FINALITY=5
export FOREST_CHAIN_INDEXER_ENABLED=true
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
forest-cli net connect "$(cat "${LOTUS_1_DATA_DIR}/lotus-1-ipv4addr")" || true
forest-cli net connect "$(cat "${LOTUS_2_DATA_DIR}/lotus-2-ipv4addr")" || true

# Ensure the Forest node is fully synced before proceeding
forest-cli sync wait
forest-cli sync status
forest-cli healthcheck healthy --healthcheck-port "${FOREST_HEALTHZ_RPC_PORT}"
echo "forest: ready."
sleep infinity
