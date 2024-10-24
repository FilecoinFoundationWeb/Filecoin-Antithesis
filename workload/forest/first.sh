#!/bin/bash

set -e

INIT_BLOCK_HEIGHT="${INIT_BLOCK_HEIGHT:-20}"
RPC_FOREST="${RPC_FOREST:-http://10.20.20.26:3456/rpc/v0}"

# Waiting for rpc endpoint to come online
RPC_READY=0
while [ $RPC_READY -eq 0 ]
do
    echo "Workload [Forest][first.sh]: Waiting for RPC endpoint (ChainHead at ${RPC_FOREST}) to come online..."
    RPC_READY=$(curl -X POST $RPC_FOREST -H 'Content-Type: application/json' --data '{"jsonrpc":"2.0","id":1,"method":"Filecoin.ChainHead","params":[]}' | grep result | wc -l)
    sleep 5
done
echo "Workload [Forest][first.sh]: ChainHead RPC endpoint online"

# Waiting for the chain head to pass a certain height
BLOCK_HEIGHT_REACHED=0
while [ $INIT_BLOCK_HEIGHT -gt $BLOCK_HEIGHT_REACHED ]
do
    echo "Workload [Forest][first.sh]: Waiting for block to reach ${INIT_BLOCK_HEIGHT}"
    BLOCK_HEIGHT_REACHED=$(curl -X POST $RPC_FOREST -H 'Content-Type: application/json' --data '{"jsonrpc":"2.0","id":1,"method":"Filecoin.ChainHead","params":[]}' | jq '.result.Height')
    echo "Workload [Forest][first.sh]: Block height check: reached ${BLOCK_HEIGHT_REACHED}"
    sleep 5
done
echo "Workload [Forest][first.sh]: Chain head has reached target block height"

echo "Workload [Forest][first.sh]: Initializing wallets..."
python3 -u /opt/antithesis/resources/initialize_wallets.py "forest"

echo "Workload [Forest][first.sh]: Completed workload setup."
