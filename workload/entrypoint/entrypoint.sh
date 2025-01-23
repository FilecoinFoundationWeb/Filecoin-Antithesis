#!/bin/bash

set -e

RPC_LOTUS="${RPC_LOTUS:-http://10.20.20.24:1234/rpc/v0}"

# Waiting for the chain head to pass a certain height
INIT_BLOCK_HEIGHT="${INIT_BLOCK_HEIGHT:-10}"
BLOCK_HEIGHT_REACHED=0

echo "Workload [entrypoint]: waiting for block height to reach ${INIT_BLOCK_HEIGHT}"

while [ $INIT_BLOCK_HEIGHT -gt $BLOCK_HEIGHT_REACHED ]
do
    BLOCK_HEIGHT_REACHED=$(curl -X POST $RPC_LOTUS -H 'Content-Type: application/json' --data '{"jsonrpc":"2.0","id":1,"method":"Filecoin.ChainHead","params":[]}' | jq '.result.Height')
    echo "Workload [entrypoint]: block height check: reached ${BLOCK_HEIGHT_REACHED}"
    if [ $INIT_BLOCK_HEIGHT -le $BLOCK_HEIGHT_REACHED ]; then
        break
    fi
    sleep 5
done

echo "Workload [entrypoint]: chainhead has reached block height ${INIT_BLOCK_HEIGHT}"

python3 -u /opt/antithesis/entrypoint/setup_complete.py

sleep infinity