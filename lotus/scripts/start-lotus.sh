#!/bin/bash

no="$1"

lotus_data_dir="LOTUS_${no}_DATA_DIR"
export LOTUS_DATA_DIR="${!lotus_data_dir}"

lotus_ip="LOTUS_${no}_IP"
export LOTUS_IP="${!lotus_ip}"

lotus_rpc_port="LOTUS_${no}_RPC_PORT"
export LOTUS_RPC_PORT="${!lotus_rpc_port}"

export LOTUS_F3_BOOTSTRAP_EPOCH=21
export LOTUS_CHAININDEXER_ENABLEINDEXER=true
export DRAND0_IP=${DRAND0_IP}

# required via docs:
lotus_path="LOTUS_${no}_PATH"
export LOTUS_PATH="${!lotus_path}"

lotus_miner_path="LOTUS_MINER_${no}_PATH"
export LOTUS_MINER_PATH="${!lotus_miner_path}"

export LOTUS_SKIP_GENESIS_CHECK=${LOTUS_SKIP_GENESIS_CHECK}
export CGO_CFLAGS_ALLOW="-D__BLST_PORTABLE__"
export CGO_CFLAGS="-D__BLST_PORTABLE__"

if [ ! -f "${LOTUS_DATA_DIR}/config.toml" ]; then
    INIT_MODE=true
else
    INIT_MODE=false
fi

echo "lotus${no}: Fetching drand chain info from ${DRAND0_IP}..."

tries=10
while [ "$tries" -gt 0 ]; do
    response=$(curl -s --fail "http://${DRAND0_IP}/info" 2>&1)
    
    if [ $? -eq 0 ] && echo "$response" | jq -e '.public_key?' >/dev/null 2>&1; then
        echo "$response" | jq -c > chain_info
        export DRAND_CHAIN_INFO=$(pwd)/chain_info
        echo "lotus${no}: Drand chain info ready"
        break
    else
        sleep 2
        tries=$(( tries - 1 ))
    fi
done

if [ ! -f "chain_info" ]; then
    echo "lotus${no}: ERROR - Failed to get drand chain info"
    exit 1
fi
if [ "$INIT_MODE" = "true" ]; then
    sed "s/\${LOTUS_IP}/$LOTUS_IP/g; s/\${LOTUS_RPC_PORT}/$LOTUS_RPC_PORT/g" config.toml.template > config.toml

    if [ "$no" -eq 0 ]; then
        ./scripts/setup-genesis.sh
    fi

    cat ${SHARED_CONFIGS}/localnet.json | jq -r '.NetworkName' > ${LOTUS_DATA_DIR}/network_name
        
    if [ "$no" -eq 0 ]; then
        lotus --repo="${LOTUS_PATH}" daemon --lotus-make-genesis=${SHARED_CONFIGS}/devgen.car --genesis-template=${SHARED_CONFIGS}/localnet.json --bootstrap=false --config=config.toml&
    else
        lotus --repo="${LOTUS_PATH}" daemon --genesis=${SHARED_CONFIGS}/devgen.car --bootstrap=false --config=config.toml&
    fi
else
    lotus --repo="${LOTUS_PATH}" daemon --bootstrap=false --config=config.toml&
fi


lotus --version
lotus wait-api

lotus net listen > ${LOTUS_DATA_DIR}/ipv4addr
cat ${LOTUS_DATA_DIR}/ipv4addr | awk 'NR==1 {print; exit}' > ${LOTUS_DATA_DIR}/lotus${no}-ipv4addr
lotus net id > ${LOTUS_DATA_DIR}/lotus${no}-p2pID
lotus auth create-token --perm admin > ${LOTUS_DATA_DIR}/lotus${no}-jwt

retries=6
for peer in "${LOTUS_DATA_DIR}/lotus${no}-ipv4addr" "${FOREST_0_DATA_DIR}/forest-listen-addr"; do
    if [ -f "$peer" ]; then
        attempt=1
        while [ $attempt -le $retries ]; do
            if lotus net connect $(cat $peer); then
                break
            fi
            attempt=$((attempt + 1))
            sleep 5
        done
    fi
done

sleep infinity
