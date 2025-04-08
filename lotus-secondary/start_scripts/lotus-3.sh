#!/bin/bash

export LOTUS_F3_BOOTSTRAP_EPOCH=21
export LOTUS_PATH=${LOTUS_3_PATH}
export LOTUS_SKIP_GENESIS_CHECK=${LOTUS_SKIP_GENESIS_CHECK}
export CGO_CFLAGS_ALLOW="-D__BLST_PORTABLE__" 
export CGO_CFLAGS="-D__BLST_PORTABLE__"
export LOTUS_CHAININDEXER_ENABLEINDEXER=true
curl 10.20.20.21/info | jq -c > chain_info
export DRAND_CHAIN_INFO=chain_info

lotus --version

# Create necessary directories
mkdir -p ${LOTUS_3_DATA_DIR}

# Copy configuration
cp /lotus_instrumented/customer/config-3.toml "${LOTUS_3_DATA_DIR}/config.toml"
cat localnet.json | jq -r '.NetworkName' > ${LOTUS_3_DATA_DIR}/network_name
cp localnet.json ${LOTUS_3_DATA_DIR}/localnet.json

# Start lotus daemon
lotus --repo="${LOTUS_3_PATH}" daemon --genesis=${LOTUS_1_DATA_DIR}/devgen.car --bootstrap=false --config=${LOTUS_3_DATA_DIR}/config.toml &

# Wait for API to be ready
lotus wait-api

# Save node information
lotus net listen > ${LOTUS_3_DATA_DIR}/ipv4addr
lotus net id > ${LOTUS_3_DATA_DIR}/p2pID
lotus auth create-token --perm admin > ${LOTUS_3_DATA_DIR}/jwt

# Connect to existing nodes
lotus net connect $(cat ${LOTUS_1_DATA_DIR}/ipv4addr)
lotus net connect $(cat ${LOTUS_2_DATA_DIR}/ipv4addr)
sleep 5
lotus net connect $(cat ${FOREST_DATA_DIR}/ipv4addr)

sleep infinity 