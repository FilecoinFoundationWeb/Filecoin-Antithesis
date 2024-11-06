#!/bin/bash

# Wait for 90 seconds
sleep 90

# Export environment variables
export LOTUS_F3_BOOTSTRAP_EPOCH=901
export DRAND_CHAIN_INFO=chain_info
export LOTUS_PATH=${LOTUS_2_PATH}
export LOTUS_MINER_PATH=${LOTUS_2_MINER_PATH}
export LOTUS_SKIP_GENESIS_CHECK=_yes_ 
export CGO_CFLAGS_ALLOW="-D__BLST_PORTABLE__" 
export CGO_CFLAGS="-D__BLST_PORTABLE__" 
echo $LOTUS_PATH
# Check if the LOTUS_PATH exists
if [ ! -d "$LOTUS_PATH" ]; then
    echo "LOTUS_PATH does not exist: $LOTUS_PATH"
    exit 1
fi

# Run lotus-miner commands with the correct environment variables
LOTUS_PATH=${LOTUS_2_PATH} LOTUS_MINER_PATH=${LOTUS_2_MINER_PATH} lotus-miner --version
echo $LOTUS_PATH
LOTUS_PATH=${LOTUS_2_PATH} LOTUS_MINER_PATH=${LOTUS_2_MINER_PATH} lotus wallet import --as-default ~/.genesis-sectors2/pre-seal-t01001.key 
LOTUS_PATH=${LOTUS_2_PATH} LOTUS_MINER_PATH=${LOTUS_2_MINER_PATH} lotus-miner init --genesis-miner --actor=t01001 --sector-size=2KiB --pre-sealed-sectors=/root/.genesis-sectors2 --pre-sealed-metadata=manifest.json --nosync
# echo "lotus-miner: setup complete"
# LOTUS_PATH=${LOTUS_2_PATH} LOTUS_MINER_PATH=${LOTUS_2_MINER_PATH} lotus-miner run --nosync