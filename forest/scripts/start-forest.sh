#!/bin/bash

node_number="$1"

forest_data_dir="FOREST_${node_number}_DATA_DIR"
export FOREST_DATA_DIR="${!forest_data_dir}"
export LD_LIBRARY_PATH="/usr/local/lib:${LD_LIBRARY_PATH}"

export FOREST_RPC_PORT=$FOREST_RPC_PORT
export FOREST_P2P_PORT=$FOREST_P2P_PORT
export FOREST_HEALTHZ_RPC_PORT=$FOREST_HEALTHZ_RPC_PORT
export FOREST_TARGET_PEER_COUNT=$(($NUM_LOTUS_CLIENTS + $NUM_FOREST_CLIENTS - 1))

f3_sidecar_var="FOREST_${node_number}_F3_SIDECAR_RPC_ENDPOINT"
export FOREST_F3_SIDECAR_RPC_ENDPOINT="${!f3_sidecar_var}"

export FOREST_F3_BOOTSTRAP_EPOCH=5
export FOREST_F3_FINALITY=2
export FOREST_CHAIN_INDEXER_ENABLED=true
export FOREST_BLOCK_DELAY_SECS=4
export FOREST_PROPAGATION_DELAY_SECS=1

MAX_DRAND_RETRIES=60
drand_attempt=0
while true; do
    echo "forest${node_number}: Fetching drand chain info from drand0..."
    response=$(curl -s --fail "http://drand0/info" 2>&1)

    if [ $? -eq 0 ] && echo "$response" | jq -e '.public_key?' >/dev/null 2>&1; then
        # Forest expects drand config as {servers, chain_info, network_type}
        formatted_json=$(jq --arg server "http://drand0" '{ servers: [$server], chain_info: { public_key: .public_key, period: .period, genesis_time: .genesis_time, hash: .hash, groupHash: .groupHash }, network_type: "Quicknet" }' <<<"$response")
        echo "formatted_json: $formatted_json"
        export FOREST_DRAND_QUICKNET_CONFIG="$formatted_json"
        echo "forest${node_number}: Drand chain info ready"
        break
    else
        drand_attempt=$((drand_attempt + 1))
        if [ "$drand_attempt" -ge "$MAX_DRAND_RETRIES" ]; then
            echo "ERROR: Timed out waiting for drand0 after $MAX_DRAND_RETRIES attempts"
            exit 1
        fi
        sleep 2
    fi
done

export NETWORK_NAME=$(jq -r '.NetworkName' "${SHARED_CONFIGS}/localnet.json")

forest --version

host_ip=$(getent hosts "forest${node_number}" | awk '{ print $1 }')

generate_forest_config() {
    sed "s|\${FOREST_DATA_DIR}|$FOREST_DATA_DIR|g; s|\${FOREST_TARGET_PEER_COUNT}|$FOREST_TARGET_PEER_COUNT|g" \
        /forest/forest_config.toml.tpl > "${FOREST_DATA_DIR}/forest_config.toml"
    echo "name = \"${NETWORK_NAME}\"" >> "${FOREST_DATA_DIR}/forest_config.toml"
}

if [ ! -f "${FOREST_DATA_DIR}/jwt" ]; then
    generate_forest_config

    echo "---------------------------"
    echo "ip address: $host_ip"
    echo "---------------------------"

    forest --genesis "${SHARED_CONFIGS}/devgen.car" \
        --config "${FOREST_DATA_DIR}/forest_config.toml" \
        --save-token "${FOREST_DATA_DIR}/jwt" \
        --no-healthcheck \
        --skip-load-actors \
        --exit-after-init
else
    echo "forest${node_number}: Node already initialized, skipping init..."
    generate_forest_config
fi

# Launch daemon with retry — on restart the network interface may not be ready yet,
# causing bind failures. Retry up to 10 times with backoff.
launch_forest() {
    forest --genesis "${SHARED_CONFIGS}/devgen.car" \
           --config "${FOREST_DATA_DIR}/forest_config.toml" \
           --rpc-address "${host_ip}:${FOREST_RPC_PORT}" \
           --p2p-listen-address "/ip4/${host_ip}/tcp/${FOREST_P2P_PORT}" \
           --healthcheck-address "${host_ip}:${FOREST_HEALTHZ_RPC_PORT}" &
    FOREST_PID=$!
}

MAX_LAUNCH_RETRIES=10
for (( attempt=1; attempt<=MAX_LAUNCH_RETRIES; attempt++ )); do
    launch_forest
    sleep 3
    if kill -0 "$FOREST_PID" 2>/dev/null; then
        echo "forest${node_number}: daemon started (pid=$FOREST_PID)"
        break
    fi
    echo "forest${node_number}: daemon exited early (attempt $attempt/$MAX_LAUNCH_RETRIES), retrying in 5s..."
    sleep 5
done

if ! kill -0 "$FOREST_PID" 2>/dev/null; then
    echo "ERROR: forest${node_number} daemon failed to start after $MAX_LAUNCH_RETRIES attempts"
    exit 1
fi

export TOKEN=$(cat "${FOREST_DATA_DIR}/jwt")
export FULLNODE_API_INFO="$TOKEN:/ip4/$host_ip/tcp/${FOREST_RPC_PORT}/http"
echo "FULLNODE_API_INFO: $FULLNODE_API_INFO"

forest-cli wait-api
echo "forest${node_number}: collecting network info…"

forest-cli net listen | grep -v "127.0.0.1" | grep -v "::1" | head -n 1 > "${FOREST_DATA_DIR}/forest${node_number}-ipv4addr"
cp "${FOREST_DATA_DIR}/jwt" "${FOREST_DATA_DIR}/forest${node_number}-jwt"
forest-cli net id > "${FOREST_DATA_DIR}/forest${node_number}-p2pid" 2>/dev/null || echo "WARNING: P2P ID export failed"

echo "forest${node_number}: Exported artifacts to ${FOREST_DATA_DIR}:"
ls -la "${FOREST_DATA_DIR}/forest${node_number}-"* 2>/dev/null || true

echo "forest${node_number}: Importing genesis miner keys..."
for PRESEAL_KEY_FILE in ${SHARED_CONFIGS}/.genesis-sector-*/pre-seal-*.key; do
    if [ -f "$PRESEAL_KEY_FILE" ]; then
        echo "Importing pre-seal key from $PRESEAL_KEY_FILE"
        forest-wallet --remote-wallet import "$PRESEAL_KEY_FILE" || true
    fi
done

connect_with_retries() {
    local max_retries=10
    local addr_file="$1"

    for (( j=1; j<=max_retries; j++ )); do
        echo "attempt $j/$max_retries..."

        ip=$(<"$addr_file")

        if forest-cli net connect "$ip"; then
            echo "successful connect!"
            return 0
        else
            sleep 2
        fi
    done

    echo "ERROR: reached $max_retries attempts."
    return 1
}

echo "forest${node_number}: connecting to lotus nodes..."
for (( i=0; i<$NUM_LOTUS_CLIENTS; i++ )); do
    lotus_data_dir="LOTUS_${i}_DATA_DIR"
    LOTUS_DATA_DIR="${!lotus_data_dir}"
    addr_file="${LOTUS_DATA_DIR}/lotus${i}-ipv4addr"

    echo "Connecting to lotus$i at $addr_file"
    connect_with_retries "$addr_file"
done

echo "forest${node_number}: connecting to other forest nodes..."
for (( i=0; i<$NUM_FOREST_CLIENTS; i++ )); do
    if [[ $i -eq $node_number ]]; then
        continue
    fi

    other_forest_data_dir="FOREST_${i}_DATA_DIR"
    OTHER_FOREST_DATA_DIR="${!other_forest_data_dir}"
    addr_file="${OTHER_FOREST_DATA_DIR}/forest${i}-ipv4addr"

    echo "Connecting to forest$i at $addr_file"
    connect_with_retries "$addr_file"
done

forest-cli sync wait
forest-cli sync status
forest-cli healthcheck healthy --healthcheck-port "${FOREST_HEALTHZ_RPC_PORT}"

echo "forest${node_number}: completed startup"

wait $FOREST_PID
