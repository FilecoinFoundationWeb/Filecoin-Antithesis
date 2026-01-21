#!/bin/bash

no="$1"

lotus_data_dir="LOTUS_${no}_DATA_DIR"
export LOTUS_DATA_DIR="${!lotus_data_dir}"

export LOTUS_F3_BOOTSTRAP_EPOCH=21
export LOTUS_CHAININDEXER_ENABLEINDEXER=true

# required via docs:
lotus_path="LOTUS_${no}_PATH"
export LOTUS_PATH="${!lotus_path}"

lotus_miner_path="LOTUS_MINER_${no}_PATH"
export LOTUS_MINER_PATH="${!lotus_miner_path}"

export LOTUS_RPC_PORT=$LOTUS_RPC_PORT
export LOTUS_SKIP_GENESIS_CHECK=${LOTUS_SKIP_GENESIS_CHECK}
export CGO_CFLAGS_ALLOW="-D__BLST_PORTABLE__"
export CGO_CFLAGS="-D__BLST_PORTABLE__"

if [ ! -f "${LOTUS_DATA_DIR}/config.toml" ]; then
    INIT_MODE=true
else
    INIT_MODE=false
fi

while true; do
    echo "lotus${no}: Fetching drand chain info from drand0..."
    response=$(curl -s --fail "http://drand0/info" 2>&1)
    
    if [ $? -eq 0 ] && echo "$response" | jq -e '.public_key?' >/dev/null 2>&1; then
        echo "$response" | jq -c > chain_info
        echo "$response"
        export DRAND_CHAIN_INFO=$(pwd)/chain_info
        echo "lotus${no}: Drand chain info ready"
        break
    else
        sleep 2
    fi
done

if [ "$INIT_MODE" = "true" ]; then
    host_ip=$(getent hosts "lotus${no}" | awk '{ print $1 }')

    echo "---------------------------"
    echo "ip address: $host_ip"
    echo "---------------------------"

    sed "s|\${host_ip}|$host_ip|g; s|\${LOTUS_RPC_PORT}|$LOTUS_RPC_PORT|g" config.toml.template > config.toml

    if [ "$no" -eq 0 ]; then
        ./scripts/setup-genesis.sh
    fi

    cat ${SHARED_CONFIGS}/localnet.json | jq -r '.NetworkName' > ${LOTUS_DATA_DIR}/network_name

    if [ "$no" -eq 0 ]; then
        # TODO: This step is FLAKY!
        # The error message we see is the following:
        # 
        # genesis func failed: make genesis block: failed to verify presealed data: failed to create verifier: failed to call method: message failed with backtrace:
        # 00: f06 (method 2) -- Allowance 0 below minimum deal size for add verifier f081 (16)
        #
        # Is there a way to resolve this?
        lotus --repo="${LOTUS_PATH}" daemon --lotus-make-genesis=${SHARED_CONFIGS}/devgen.car --genesis-template=${SHARED_CONFIGS}/localnet.json --bootstrap=false --config=config.toml&
    else
        lotus --repo="${LOTUS_PATH}" daemon --genesis=${SHARED_CONFIGS}/devgen.car --bootstrap=false --config=config.toml&
    fi
else
    lotus --repo="${LOTUS_PATH}" daemon --bootstrap=false --config=config.toml&
fi

lotus --version
lotus wait-api

lotus net listen | grep -v "127.0.0.1" | grep -v "::1" | head -n 1 > ${LOTUS_DATA_DIR}/lotus${no}-ipv4addr
lotus net id > ${LOTUS_DATA_DIR}/lotus${no}-p2pID
if [ ! -f "${LOTUS_DATA_DIR}/lotus${no}-jwt" ]; then
    lotus auth create-token --perm admin > ${LOTUS_DATA_DIR}/lotus${no}-jwt
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

    echo "ERROR: reached $MAX_RETRIES attempts."
    return 1
}

echo "connecting to other lotus nodes..."
for (( i=0; i<$NUM_LOTUS_CLIENTS; i++ )); do
    if [[ $i -eq $no ]]; then
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

echo "lotus${no}: completed startup"

sleep infinity
