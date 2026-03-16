#!/bin/bash

node_number="$1"

lotus_data_dir="LOTUS_${node_number}_DATA_DIR"
export LOTUS_DATA_DIR="${!lotus_data_dir}"

export LOTUS_CHAININDEXER_ENABLEINDEXER=true

lotus_path="LOTUS_${node_number}_PATH"
export LOTUS_PATH="${!lotus_path}"

lotus_miner_path="LOTUS_MINER_${node_number}_PATH"
if [ -n "${!lotus_miner_path:-}" ]; then
    export LOTUS_MINER_PATH="${!lotus_miner_path}"
fi

export LOTUS_RPC_PORT=$LOTUS_RPC_PORT
export LOTUS_SKIP_GENESIS_CHECK=${LOTUS_SKIP_GENESIS_CHECK}
export CGO_CFLAGS_ALLOW="-D__BLST_PORTABLE__"
export CGO_CFLAGS="-D__BLST_PORTABLE__"

if [ ! -f "${LOTUS_DATA_DIR}/config.toml" ]; then
    INIT_MODE=true
else
    INIT_MODE=false
fi

MAX_DRAND_RETRIES=60
drand_attempt=0
while true; do
    echo "lotus${node_number}: Fetching drand chain info from drand0..."
    response=$(curl -s --fail "http://drand0/info" 2>&1)

    if [ $? -eq 0 ] && echo "$response" | jq -e '.public_key?' >/dev/null 2>&1; then
        echo "$response" | jq -c > chain_info
        echo "$response"
        export DRAND_CHAIN_INFO=$(pwd)/chain_info
        echo "lotus${node_number}: Drand chain info ready"
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

if [ "$INIT_MODE" = "true" ]; then
    host_ip=$(getent hosts "lotus${node_number}" | awk '{ print $1 }')

    echo "---------------------------"
    echo "ip address: $host_ip"
    echo "---------------------------"

    sed "s|\${host_ip}|$host_ip|g; s|\${LOTUS_RPC_PORT}|$LOTUS_RPC_PORT|g" config.toml.template > config.toml

    if [ "$node_number" -eq 0 ]; then
        ./scripts/setup-genesis.sh
    fi

    jq -r '.NetworkName' "${SHARED_CONFIGS}/localnet.json" > "${LOTUS_DATA_DIR}/network_name"

    if [ "$node_number" -eq 0 ]; then
        lotus --repo="${LOTUS_PATH}" daemon --lotus-make-genesis=${SHARED_CONFIGS}/devgen.car --genesis-template=${SHARED_CONFIGS}/localnet.json --bootstrap=false --config=config.toml&
    else
        lotus --repo="${LOTUS_PATH}" daemon --genesis=${SHARED_CONFIGS}/devgen.car --bootstrap=false --config=config.toml&
    fi
else
    lotus --repo="${LOTUS_PATH}" daemon --bootstrap=false --config=config.toml&
fi

lotus --version
lotus wait-api

lotus net listen | grep -v "127.0.0.1" | grep -v "::1" | head -n 1 > "${LOTUS_DATA_DIR}/lotus${node_number}-ipv4addr"
lotus net id > "${LOTUS_DATA_DIR}/lotus${node_number}-p2pID"
if [ ! -f "${LOTUS_DATA_DIR}/lotus${node_number}-jwt" ]; then
    lotus auth create-token --perm admin > "${LOTUS_DATA_DIR}/lotus${node_number}-jwt"
fi

connect_with_retries() {
    local max_retries=10
    local addr_file="$1"

    for (( j=1; j<=max_retries; j++ )); do
        echo "attempt $j/$max_retries..."

        ip=$(<"$addr_file")
        if lotus net connect "$ip"; then
            echo "successful connect!"
            return 0
        else
            sleep 2
        fi
    done

    echo "ERROR: reached $max_retries attempts."
    return 1
}

echo "connecting to other lotus nodes..."
for (( i=0; i<$NUM_LOTUS_CLIENTS; i++ )); do
    if [[ $i -eq $node_number ]]; then
        continue
    fi

    other_lotus_data_dir="LOTUS_${i}_DATA_DIR"
    OTHER_LOTUS_DATA_DIR="${!other_lotus_data_dir}"
    addr_file="${OTHER_LOTUS_DATA_DIR}/lotus${i}-ipv4addr"

    echo "Connecting to lotus$i at $addr_file"
    connect_with_retries "$addr_file"
done

echo "connecting to forest nodes..."
for (( i=0; i<$NUM_FOREST_CLIENTS; i++ )); do
    forest_data_dir="FOREST_${i}_DATA_DIR"
    FOREST_DATA_DIR="${!forest_data_dir}"
    addr_file="${FOREST_DATA_DIR}/forest${i}-ipv4addr"

    echo "Connecting to forest$i at $addr_file"
    connect_with_retries "$addr_file"
done

echo "lotus${node_number}: completed startup"

sleep infinity
