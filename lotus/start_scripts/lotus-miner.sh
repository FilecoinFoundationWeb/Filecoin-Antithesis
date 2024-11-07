#!/bin/bash

sleep 40

# # Waiting for lotus node to be up
# lotus_node_ready=0
# while [[ ${lotus_node_ready?} -eq 0 ]]
# do
#     echo "lotus-miner: checking if lotus-node is ready.."
#     if [[ -e "/container_ready/lotus-node" ]]
#     then
#         echo "lotus-miner: lotus-node is ready!"
#         echo "lotus-miner: continuing startup..."
#         lotus_node_ready=1
#         break
#     fi
#     sleep 5
# done
export LOTUS_F3_BOOTSTRAP_EPOCH=901
export DRAND_CHAIN_INFO=chain_info
export LOTUS_PATH=${LOTUS_1_PATH}
export LOTUS_MINER_PATH=${LOTUS_1_MINER_PATH}
export LOTUS_SKIP_GENESIS_CHECK=_yes_
export CGO_CFLAGS_ALLOW="-D__BLST_PORTABLE__"
export CGO_CFLAGS="-D__BLST_PORTABLE__"
lotus-miner --version
lotus wallet import --as-default ${LOTUS_DATA_DIR}/key
lotus-miner init --genesis-miner --actor=t01000 --sector-size=2KiB --pre-sealed-sectors=/root/.genesis-sectors --pre-sealed-metadata=manifest.json --nosync
echo "lotus-miner: setup complete"
lotus-miner run --nosync
