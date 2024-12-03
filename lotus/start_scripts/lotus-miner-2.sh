#!/bin/bash

sleep 20

# Waiting for lotus node 2 to be up
lotus_2_ready=0
while [[ ${lotus_2_ready?} -eq 0 ]]
do
    echo "lotus-miner-2: checking if lotus-2 is ready.."
    if [[ -e "/container_ready/lotus-2" ]]
    then
        echo "lotus-miner-2: lotus-2 is ready!"
        echo "lotus-miner-2: continuing startup..."
        lotus_2_ready=1
        break
    fi
    sleep 5
done

export LOTUS_F3_BOOTSTRAP_EPOCH=901
export DRAND_CHAIN_INFO=chain_info
export LOTUS_PATH=${LOTUS_2_PATH}
export LOTUS_MINER_PATH=${LOTUS_MINER_2_PATH}
export LOTUS_SKIP_GENESIS_CHECK=${LOTUS_SKIP_GENESIS_CHECK}
export CGO_CFLAGS_ALLOW="-D__BLST_PORTABLE__" 
export CGO_CFLAGS="-D__BLST_PORTABLE__"

lotus-miner --version
lotus wallet import --as-default ${LOTUS_2_DATA_DIR}/key
lotus-miner init --actor=${LOTUS_MINER_2_ACTOR_ADDRESS} --sector-size=2KiB --pre-sealed-sectors=/root/.genesis-sector-2 --pre-sealed-metadata=manifest.json --nosync
echo "lotus-miner-2: setup complete"
lotus-miner run --nosync
touch /container_ready/lotus-miner-2