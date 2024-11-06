#!/bin/bash

sleep 40
export LOTUS_F3_BOOTSTRAP_EPOCH=901
export DRAND_CHAIN_INFO=chain_info
export LOTUS_MINER_PATH=~/.lotus-miner-local-net1
export LOTUS_SKIP_GENESIS_CHECK=_yes_
export CGO_CFLAGS_ALLOW="-D__BLST_PORTABLE__"
export CGO_CFLAGS="-D__BLST_PORTABLE__"


lotus-miner --version
lotus-seed pre-seal --sector-size 2KiB --num-sectors 2 --miner-addr t01001
lotus wallet import --as-default /root/.genesis-sectors/pre-seal-t01000.key
lotus-miner init --genesis-miner --actor=t01000 --sector-size=2KiB --pre-sealed-sectors=/root/.genesis-sectors --pre-sealed-metadata=/root/.genesis-sectors/pre-seal-t01000.json --nosync
echo "lotus-miner: setup complete"
lotus-miner run --nosync