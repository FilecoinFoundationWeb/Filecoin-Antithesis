#!/bin/bash

# Function to wait for Lotus node
wait_for_lotus() {
    echo "forest: waiting for Lotus node..."
    local max_attempts=30
    local attempt=1
    while [ $attempt -le $max_attempts ]; do
        if [ -f "${LOTUS_1_DATA_DIR}/.node_ready" ] && [ -f "${LOTUS_1_DATA_DIR}/jwt" ]; then
            echo "forest: Lotus node is ready"
            return 0
        fi
        echo "forest: waiting for Lotus (attempt ${attempt}/${max_attempts})"
        sleep 2
        attempt=$((attempt + 1))
    done
    echo "forest: timed out waiting for Lotus"
    return 1
}

# Wait for Lotus to be ready
wait_for_lotus

# Common setup
echo "forest: performing setup..."
DRAND_SERVER="http://10.20.20.21"
json=$(curl -s "$DRAND_SERVER/info")
formatted_json=$(jq --arg server "$DRAND_SERVER" '{ "servers": [$server], "chain_info": { "public_key": .public_key, "period": .period, "genesis_time": .genesis_time, "hash": .hash, "groupHash": .groupHash }, "network_type": "Quicknet" }' <<<"$json")
export FOREST_DRAND_QUICKNET_CONFIG="$formatted_json"
export FOREST_F3_BOOTSTRAP_EPOCH=21
export FOREST_F3_FINALITY=10
NETWORK_NAME=$(jq -r '.NetworkName' "${LOTUS_1_DATA_DIR}/localnet.json")
export NETWORK_NAME=$NETWORK_NAME
forest --version
cp /forest/forest_config.toml.tpl "${FOREST_DATA_DIR}/forest_config.toml"
echo "name = \"${NETWORK_NAME}\"" >> "${FOREST_DATA_DIR}/forest_config.toml"

# Start Forest
echo "forest: starting forest..."
forest --genesis "${LOTUS_1_DATA_DIR}/devgen.car" \
       --config "${FOREST_DATA_DIR}/forest_config.toml" \
       --save-token "${FOREST_DATA_DIR}/jwt" \
       --rpc-address ${FOREST_IP}:${FOREST_RPC_PORT} \
       --p2p-listen-address /ip4/${FOREST_IP}/tcp/${FOREST_P2P_PORT} \
       --healthcheck-address ${FOREST_IP}:${FOREST_HEALTHZ_RPC_PORT} &
FOREST_PID=$!

# Wait for RPC to be ready
echo "forest: waiting for RPC..."
max_attempts=30
attempt=1
rpc_ready=false
while [ $attempt -le $max_attempts ]; do
    if curl -s -f "http://${FOREST_IP}:${FOREST_HEALTHZ_RPC_PORT}/livez" > /dev/null; then
        echo "forest: RPC is ready"
        rpc_ready=true
        break
    fi
    echo "forest: waiting for RPC (attempt ${attempt}/${max_attempts})"
    sleep 2
    attempt=$((attempt + 1))
done

if [ "$rpc_ready" = false ]; then
    echo "forest: RPC did not become ready in time. Exiting."
    exit 1
fi

export TOKEN=$(cat ${FOREST_DATA_DIR}/jwt)
export FULLNODE_API_INFO=$TOKEN:/ip4/${FOREST_IP}/tcp/${FOREST_RPC_PORT}/http
echo "FULLNODE_API_INFO: $FULLNODE_API_INFO"

echo "forest: collecting network info…"
forest-cli net listen | head -n1 > "${FOREST_DATA_DIR}/forest-listen-addr"

echo "forest: connecting to lotus nodes…"
# Import wallets in parallel
forest-wallet --remote-wallet import ${LOTUS_1_DATA_DIR}/key || true &
forest-wallet --remote-wallet import ${LOTUS_2_DATA_DIR}/key || true &
wait

# Connect to Lotus nodes
echo "forest: connecting to Lotus nodes..."
lotus1_addr=$(cat ${LOTUS_1_DATA_DIR}/lotus-1-ipv4addr)
lotus2_addr=$(cat ${LOTUS_2_DATA_DIR}/lotus-2-ipv4addr)

# Try connecting to both nodes with retries
max_attempts=5
attempt=1
connected_lotus1=false
connected_lotus2=false
while [ $attempt -le $max_attempts ]; do
    if [ "$connected_lotus1" = false ] && forest-cli net connect "$lotus1_addr"; then
        echo "forest: successfully connected to Lotus-1"
        connected_lotus1=true
    elif [ "$connected_lotus1" = false ]; then
        echo "forest: failed to connect to Lotus-1 (attempt ${attempt}/${max_attempts})"
    fi

    if [ "$connected_lotus2" = false ] && forest-cli net connect "$lotus2_addr"; then
        echo "forest: successfully connected to Lotus-2"
        connected_lotus2=true
    elif [ "$connected_lotus2" = false ]; then
        echo "forest: failed to connect to Lotus-2 (attempt ${attempt}/${max_attempts})"
    fi
    
    if [ "$connected_lotus1" = true ] && [ "$connected_lotus2" = true ]; then
        echo "forest: successfully connected to both Lotus nodes"
        break
    fi
    
    echo "forest: retrying connections in 2 seconds..."
    sleep 2
    attempt=$((attempt + 1))
done

# Start sync
echo "forest: starting sync..."
forest-cli sync wait 

echo "forest: ready."
wait $FOREST_PID
