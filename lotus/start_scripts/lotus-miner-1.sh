#!/bin/bash

export LOTUS_F3_BOOTSTRAP_EPOCH=21
export DRAND_CHAIN_INFO=${LOTUS_1_DATA_DIR}/chain_info
export LOTUS_PATH=${LOTUS_1_PATH}
export LOTUS_MINER_PATH=${LOTUS_MINER_1_PATH}
export LOTUS_SKIP_GENESIS_CHECK=${LOTUS_SKIP_GENESIS_CHECK}
export CGO_CFLAGS_ALLOW="-D__BLST_PORTABLE__"
export CGO_CFLAGS="-D__BLST_PORTABLE__"

# Check if initialization is needed
INIT_FLAG_FILE=${LOTUS_MINER_1_PATH}/.initialized

if [ ! -f "$INIT_FLAG_FILE" ]; then
    echo "lotus-miner-1: Running in initialization mode..."
    lotus-miner --version
    lotus wallet import --as-default ${LOTUS_1_DATA_DIR}/key
    lotus-miner init --genesis-miner --actor=${LOTUS_MINER_1_ACTOR_ADDRESS} --sector-size=2KiB --pre-sealed-sectors=/root/.genesis-sector-1 --pre-sealed-metadata=manifest.json --nosync
    touch "$INIT_FLAG_FILE"
    echo "lotus-miner-1: setup complete"
else
    echo "lotus-miner-1: Already initialized, starting daemon..."
fi

lotus-miner run --nosync &

sleep infinity