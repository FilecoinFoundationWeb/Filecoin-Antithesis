#!/bin/bash

sleep 20

# Waiting for lotus node 1 to be up
lotus_1_ready=0
while [[ ${lotus_1_ready?} -eq 0 ]]
do
    echo "lotus-miner-1: checking if lotus-1 is ready.."
    if [[ -e "/container_ready/lotus-1" ]]
    then
        echo "lotus-miner-1: lotus-1 is ready!"
        echo "lotus-miner-1: continuing startup..."
        lotus_1_ready=1
        break
    fi
    sleep 5
done

export LOTUS_F3_BOOTSTRAP_EPOCH=901
export DRAND_CHAIN_INFO=chain_info
export LOTUS_PATH=${LOTUS_1_PATH}
export LOTUS_MINER_PATH=${LOTUS_MINER_1_PATH}
export LOTUS_SKIP_GENESIS_CHECK=${LOTUS_SKIP_GENESIS_CHECK}
export CGO_CFLAGS_ALLOW="-D__BLST_PORTABLE__"
export CGO_CFLAGS="-D__BLST_PORTABLE__"

lotus-miner --version
lotus wallet import --as-default ${LOTUS_1_DATA_DIR}/key
lotus-miner init --genesis-miner --actor=${LOTUS_MINER_1_ACTOR_ADDRESS} --sector-size=2KiB --pre-sealed-sectors=/root/.genesis-sector-1 --pre-sealed-metadata=manifest.json --nosync
echo "lotus-miner-1: setup complete"
lotus-miner run --nosync
touch /container_ready/lotus-miner-1
