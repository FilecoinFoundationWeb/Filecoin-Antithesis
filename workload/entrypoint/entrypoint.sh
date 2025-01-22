#!/bin/bash

# Waiting for the chain head to pass a certain height
INIT_BLOCK_HEIGHT="${INIT_BLOCK_HEIGHT:-10}"
RPC_FOREST="${RPC_FOREST:-http://10.20.20.26:3456/rpc/v0}"
BLOCK_HEIGHT_REACHED=0

echo "Workload [main][first.sh]: waiting for block height to reach ${INIT_BLOCK_HEIGHT}"

while [ $INIT_BLOCK_HEIGHT -gt $BLOCK_HEIGHT_REACHED ]
do
    BLOCK_HEIGHT_REACHED=$(curl -X POST $RPC_FOREST -H 'Content-Type: application/json' --data '{"jsonrpc":"2.0","id":1,"method":"Filecoin.ChainHead","params":[]}' | jq '.result.Height')
    echo "Workload [main][first.sh]: block height check: reached ${BLOCK_HEIGHT_REACHED}"
    if [ $INIT_BLOCK_HEIGHT -le $BLOCK_HEIGHT_REACHED ]; then
        break
    fi
    sleep 5
done

echo "Workload [main][first.sh]: chainhead has reached block height ${INIT_BLOCK_HEIGHT}"

python3 -u /opt/antithesis/entrypoint/setup_complete.py

sleep infinity