#!/bin/bash

set -e

INIT_BLOCK_HEIGHT="${INIT_BLOCK_HEIGHT:-50}"
RPC_LOTUS1="${RPC_LOTUS1:-http://10.20.20.24:1234/rpc/v0}"
RPC_LOTUS2="${RPC_LOTUS2:-http://10.20.20.25:1234/rpc/v0}"

sleep 20

# Waiting for rpc endpoint to come online
RPC_READY=0
while [ $RPC_READY -eq 0 ]
do
    echo "Workload: waiting for JSON RPC endpoint (${RPC_LOTUS1}) to come online..."
    RPC_READY=$(curl -X POST $RPC_LOTUS1 -H 'Content-Type: application/json' --data '{"jsonrpc":"2.0","id":1,"method":"Filecoin.ChainHead","params":[]}' | grep result | wc -l)
    sleep 5
done
echo "Workload: JSON RPC endpoint online"

# Waiting for the chain head to pass a certain height
BLOCK_HEIGHT_REACHED=0
while [ $INIT_BLOCK_HEIGHT -gt $BLOCK_HEIGHT_REACHED ]
do
    echo "Waiting for block to reach ${INIT_BLOCK_HEIGHT}"
    BLOCK_HEIGHT_REACHED=$(curl -X POST $RPC_LOTUS1 -H 'Content-Type: application/json' --data '{"jsonrpc":"2.0","id":1,"method":"Filecoin.ChainHead","params":[]}' | jq '.result.Height')
    echo "Workload: block height check: reached ${BLOCK_HEIGHT_REACHED}"
    sleep 5
done
echo "Workload: Chain head has reached target block height"

echo "Ready to start the workload..."

# Execute the workload script
# python3 /root/devgen/fil_spammer_rpc.py 1_create_wallets
# python3 /root/devgen/fil_spammer_rpc.py 2_transfer_funds
# python3 /root/devgen/fil_spammer_rpc.py 3_transfer_between_wallets

sleep infinity
