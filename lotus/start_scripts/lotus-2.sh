#!/bin/bash

# Common environment setup
export LOTUS_F3_BOOTSTRAP_EPOCH=21
export LOTUS_PATH=${LOTUS_2_PATH}
export LOTUS_MINER_PATH=${LOTUS_MINER_2_PATH}
export LOTUS_SKIP_GENESIS_CHECK=${LOTUS_SKIP_GENESIS_CHECK}
export CGO_CFLAGS_ALLOW="-D__BLST_PORTABLE__" 
export CGO_CFLAGS="-D__BLST_PORTABLE__"
export LOTUS_CHAININDEXER_ENABLEINDEXER=true

# Check if initialization is needed by looking for key files
if [ ! -f "${LOTUS_2_DATA_DIR}/key" ] || [ ! -f "${LOTUS_2_DATA_DIR}/config.toml" ]; then
    INIT_MODE=true
    echo "lotus-2: First run detected, performing initialization..."
else
    INIT_MODE=false
    echo "lotus-2: Found existing setup, running in daemon-only mode..."
fi

# Always get fresh chain info
curl 10.20.20.21/info | jq -c > chain_info
export DRAND_CHAIN_INFO=chain_info

lotus --version

# Initialization steps
if [ "$INIT_MODE" = "true" ]; then
    echo "lotus-2: Running in initialization mode..."
    cp /root/.genesis-sector-2/pre-seal-t01001.key ${LOTUS_2_DATA_DIR}/key
    cp /lotus_instrumented/customer/config-2.toml "${LOTUS_2_DATA_DIR}/config.toml"
    cat localnet-2.json | jq -r '.NetworkName' > ${LOTUS_2_DATA_DIR}/network_name
    cp localnet-2.json ${LOTUS_2_DATA_DIR}/localnet.json
    
    # Start daemon with genesis
    lotus --repo="${LOTUS_2_PATH}" daemon --genesis=${LOTUS_1_DATA_DIR}/devgen.car --bootstrap=false --config=${LOTUS_2_DATA_DIR}/config.toml&
else
    echo "lotus-2: Running in daemon-only mode..."
    # Start daemon without genesis
    lotus --repo="${LOTUS_2_PATH}" daemon --bootstrap=false --config=${LOTUS_2_DATA_DIR}/config.toml&
fi

# Common post-startup steps
lotus wait-api
echo "lotus-2: finished waiting for API, proceeding with network setup."

lotus net listen > ${LOTUS_2_DATA_DIR}/ipv4addr
cat ${LOTUS_2_DATA_DIR}/ipv4addr | awk 'NR==1 {print; exit}' > ${LOTUS_2_DATA_DIR}/lotus-2-ipv4addr
lotus net id > ${LOTUS_2_DATA_DIR}/p2pID
lotus auth create-token --perm admin > ${LOTUS_2_DATA_DIR}/jwt

# Connect to lotus-1 if it exists
if [ -f "${LOTUS_1_DATA_DIR}/ipv4addr" ]; then
    echo "lotus-2: Connecting to lotus-1..."
    lotus net connect $(cat ${LOTUS_1_DATA_DIR}/ipv4addr)
else
    echo "lotus-2: Could not find lotus-1 address, skipping connection"
fi

sleep infinity
