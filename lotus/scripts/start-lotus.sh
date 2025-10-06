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

while true; do
    echo "lotus${no}: Fetching drand chain info from ${DRAND0_IP}..."
    response=$(curl -s --fail "http://${DRAND0_IP}/info" 2>&1)
    
    if [ $? -eq 0 ] && echo "$response" | jq -e '.public_key?' >/dev/null 2>&1; then
        echo "$response" | jq -c > chain_info
        export DRAND_CHAIN_INFO=$(pwd)/chain_info
        echo "lotus${no}: Drand chain info ready"
        break
    else
        sleep 2
    fi
done

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

# connecting to peers
retries=10
connect_with_retries() {
  local addr_file="$1"

  for (( i=1; i<=retries; i++ )); do
    echo "attempt $i..."

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
