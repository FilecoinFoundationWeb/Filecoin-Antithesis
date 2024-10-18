#!/bin/bash

set -e

INIT_BLOCK_HEIGHT="${INIT_BLOCK_HEIGHT:-10}"
RPC_LOTUS="${RPC_LOTUS:-http://10.20.20.24:1234/rpc/v0}"
#RPC_LOTUS2="${RPC_LOTUS2:-http://10.20.20.25:1234/rpc/v0}"

# Waiting for rpc endpoint to come online
RPC_READY=0
while [ $RPC_READY -eq 0 ]
do
    echo "Workload [First]: Waiting for RPC endpoint (ChainHead at ${RPC_LOTUS}) to come online..."
    RPC_READY=$(curl -X POST $RPC_LOTUS -H 'Content-Type: application/json' --data '{"jsonrpc":"2.0","id":1,"method":"Filecoin.ChainHead","params":[]}' | grep result | wc -l)
    sleep 5
done
echo "Workload [First]: ChainHead RPC endpoint online"

# Waiting for the chain head to pass a certain height
BLOCK_HEIGHT_REACHED=0
while [ $INIT_BLOCK_HEIGHT -gt $BLOCK_HEIGHT_REACHED ]
do
    echo "Workload [First]: Waiting for block to reach ${INIT_BLOCK_HEIGHT}"
    BLOCK_HEIGHT_REACHED=$(curl -X POST $RPC_LOTUS -H 'Content-Type: application/json' --data '{"jsonrpc":"2.0","id":1,"method":"Filecoin.ChainHead","params":[]}' | jq '.result.Height')
    echo "Workload [First]: Block height check: reached ${BLOCK_HEIGHT_REACHED}"
    sleep 5
done
echo "Workload [First]: Chain head has reached target block height"

echo "Workload [First]: Initializing wallets..."
python3 -u /opt/antithesis/resources/initialize_wallets.py

echo "Workload [First]: Completed workload setup."
