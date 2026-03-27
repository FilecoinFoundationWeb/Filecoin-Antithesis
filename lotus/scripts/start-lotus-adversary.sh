#!/bin/bash

# Adversarial Lotus node — joins the network as a non-mining full node.
# Uses the lotus-adversary image (may diverge from standard lotus builds).
# Usage: start-lotus-adversary.sh <node_id>

no="$1"

lotus_adversary_data_dir="LOTUS_ADVERSARY_${no}_DATA_DIR"
export LOTUS_DATA_DIR="${!lotus_adversary_data_dir}"

export LOTUS_CHAININDEXER_ENABLEINDEXER=true

lotus_adversary_path="LOTUS_ADVERSARY_${no}_PATH"
export LOTUS_PATH="${!lotus_adversary_path}"

export LOTUS_RPC_PORT=$LOTUS_RPC_PORT
export LOTUS_SKIP_GENESIS_CHECK=${LOTUS_SKIP_GENESIS_CHECK}
export CGO_CFLAGS_ALLOW="-D__BLST_PORTABLE__"
export CGO_CFLAGS="-D__BLST_PORTABLE__"

if [ ! -f "${LOTUS_DATA_DIR}/config.toml" ]; then
    INIT_MODE=true
    # Clean stale repo from previous run so we start fresh
    if [ -d "${LOTUS_PATH}" ]; then
        echo "lotus-adversary${no}: Cleaning stale repo ${LOTUS_PATH}"
        rm -rf "${LOTUS_PATH}"
    fi
else
    INIT_MODE=false
fi

while true; do
    echo "lotus-adversary${no}: Fetching drand chain info from drand0..."
    response=$(curl -s --fail "http://drand0/info" 2>&1)

    if [ $? -eq 0 ] && echo "$response" | jq -e '.public_key?' >/dev/null 2>&1; then
        echo "$response" | jq -c > chain_info
        echo "$response"
        export DRAND_CHAIN_INFO=$(pwd)/chain_info
        echo "lotus-adversary${no}: Drand chain info ready"
        break
    else
        sleep 2
    fi
done

if [ "$INIT_MODE" = "true" ]; then
    host_ip=$(getent hosts "lotus-adversary${no}" | awk '{ print $1 }')

    echo "---------------------------"
    echo "ip address: $host_ip"
    echo "---------------------------"

    sed "s|\${host_ip}|$host_ip|g; s|\${LOTUS_RPC_PORT}|$LOTUS_RPC_PORT|g" config.toml.template > config.toml

    # Wait for genesis.car from lotus0
    echo "lotus-adversary${no}: Waiting for genesis..."
    while [ ! -f "${SHARED_CONFIGS}/devgen.car" ]; do
        sleep 2
    done

    lotus --repo="${LOTUS_PATH}" daemon --genesis=${SHARED_CONFIGS}/devgen.car --bootstrap=false --config=config.toml&
else
    lotus --repo="${LOTUS_PATH}" daemon --bootstrap=false --config=config.toml&
fi

lotus --version
lotus wait-api

lotus net listen | grep -v "127.0.0.1" | grep -v "::1" | head -n 1 > ${LOTUS_DATA_DIR}/lotus-adversary${no}-ipv4addr
lotus net id > ${LOTUS_DATA_DIR}/lotus-adversary${no}-p2pID
if [ "$INIT_MODE" = "true" ] || [ ! -f "${LOTUS_DATA_DIR}/lotus-adversary${no}-jwt" ]; then
    lotus auth create-token --perm admin > ${LOTUS_DATA_DIR}/lotus-adversary${no}-jwt
fi

# connecting to peers
connect_with_retries() {
    local retries=10
    local addr_file="$1"

    for (( j=1; j<=retries; j++ )); do
        echo "attempt $j..."

        ip=$(<"$addr_file")
        if lotus net connect "$ip"; then
            echo "successful connect!"
            return 0
        else
            sleep 2
        fi
    done

    echo "ERROR: reached max attempts."
    return 1
}

echo "lotus-adversary${no}: connecting to lotus nodes..."
for (( i=0; i<$NUM_LOTUS_CLIENTS; i++ )); do
    other_lotus_data_dir="LOTUS_${i}_DATA_DIR"
    OTHER_LOTUS_DATA_DIR="${!other_lotus_data_dir}"
    addr_file="${OTHER_LOTUS_DATA_DIR}/lotus${i}-ipv4addr"

    echo "Connecting to lotus$i at $addr_file"
    if [[ ! -e "$addr_file" ]]; then
        echo "Skipping lotus$i (addr file not found — node may not be running in this profile)"
        continue
    fi
    connect_with_retries "$addr_file"
done

echo "lotus-adversary${no}: connecting to other adversary nodes..."
for (( i=0; i<$NUM_LOTUS_ADVERSARIES; i++ )); do
    if [[ $i -eq $no ]]; then
        continue
    fi

    other_adversary_data_dir="LOTUS_ADVERSARY_${i}_DATA_DIR"
    OTHER_ADVERSARY_DATA_DIR="${!other_adversary_data_dir}"
    addr_file="${OTHER_ADVERSARY_DATA_DIR}/lotus-adversary${i}-ipv4addr"

    echo "Connecting to lotus-adversary$i at $addr_file"
    if [[ ! -e "$addr_file" ]]; then
        echo "Skipping lotus-adversary$i (addr file not found — node may not be running in this profile)"
        continue
    fi
    connect_with_retries "$addr_file"
done

echo "lotus-adversary${no}: connecting to forest nodes..."
for (( i=0; i<$NUM_FOREST_CLIENTS; i++ )); do
    forest_data_dir="FOREST_${i}_DATA_DIR"
    FOREST_DATA_DIR="${!forest_data_dir}"
    addr_file="${FOREST_DATA_DIR}/forest${i}-ipv4addr"

    echo "Connecting to forest$i at $addr_file"
    if [[ ! -e "$addr_file" ]]; then
        echo "Skipping forest$i (addr file not found — node may not be running in this profile)"
        continue
    fi
    connect_with_retries "$addr_file"
done

echo "lotus-adversary${no}: completed startup"

sleep infinity
