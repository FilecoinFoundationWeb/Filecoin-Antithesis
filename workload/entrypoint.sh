#!/bin/bash

set -e

INIT_BLOCK_HEIGHT="${INIT_BLOCK_HEIGHT:-50}"
RPC_LOTUS1="${RPC_LOTUS1:-http://10.20.20.24:1234/rpc/v0}"
RPC_LOTUS2="${RPC_LOTUS2:-http://10.20.20.25:1234/rpc/v0}"

# Waiting for the chain head to pass a certain number
# We assume the RPC token is created
echo "Waiting for RPC endpoint to come online"
RPC_READY=0
while [ $RPC_READY -eq 0 ]
do
    echo "Workload: waiting for JSON RPC endpoint (${RPC_LOTUS1}) to come online"
    RPC_READY=`curl -X POST $RPC_LOTUS1 -H 'Content-Type: application/json' --data '{"jsonrpc":"2.0","id":1,"method":"Filecoin.ChainHead","params":[]}' | grep result | wc -l`
    sleep 10
done

echo "Waiting for block to reach ${INIT_BLOCK_HEIGHT}"
BLOCK_HEIGHT_REACHED=0
while [ $INIT_BLOCK_HEIGHT -gt $BLOCK_HEIGHT_REACHED ]
do
    BLOCK_HEIGHT_REACHED=$(curl -X POST $RPC_LOTUS1 -H 'Content-Type: application/json' --data '{"jsonrpc":"2.0","id":1,"method":"Filecoin.ChainHead","params":[]}' | jq '.result.Height')
    echo "Workload: block height check: reached ${BLOCK_HEIGHT_REACHED}"
    sleep 10
done

echo "Ready to start the workload"

# Executing step 1
python3 /root/fil_spammer_rpc.py 1_create_wallets

# Calling StateGetActor on one of the wallet to make sure it exist


sleep infinity
