#!/bin/bash

set -e

# RPC_LOTUS="${RPC_LOTUS:-http://10.20.20.24:1234/rpc/v1}"

# # Waiting for rpc endpoint to come online
# RPC_READY=0
# while [ $RPC_READY -eq 0 ]
# do
#     echo "Workload [main][first.sh]: waiting for lotus RPC endpoint (chainhead at ${RPC_LOTUS}) to come online..."
#     RPC_READY=$(curl -X POST $RPC_LOTUS -H 'Content-Type: application/json' --data '{"jsonrpc":"2.0","id":1,"method":"Filecoin.ChainHead","params":[]}' | grep result | wc -l)
#     if [ $RPC_READY -eq 0 ]; then
#         break
#     fi
#     sleep 5
# done
# echo "Workload [main][first.sh]: chainhead RPC endpoint online from lotus"


# RPC_FOREST="${RPC_FOREST:-http://10.20.20.26:3456/rpc/v0}"

# # Waiting for rpc endpoint to come online
# RPC_READY=0
# while [ $RPC_READY -eq 0 ]
# do
#     echo "Workload [main][first.sh]: waiting for forest RPC endpoint (chainhead at ${RPC_FOREST}) to come online..."
#     RPC_READY=$(curl -X POST $RPC_FOREST -H 'Content-Type: application/json' --data '{"jsonrpc":"2.0","id":1,"method":"Filecoin.ChainHead","params":[]}' | grep result | wc -l)
#     if [ $RPC_READY -eq 0 ]; then
#         break
#     fi
#     sleep 5
# done
# echo "Workload [main][first.sh]: chainhead RPC endpoint online from forest"


# Waiting for the chain head to pass a certain height
INIT_BLOCK_HEIGHT="${INIT_BLOCK_HEIGHT:-10}"
BLOCK_HEIGHT_REACHED=0
echo "Workload [main][first.sh]: waiting for block height to reach ${INIT_BLOCK_HEIGHT}"
while [ $INIT_BLOCK_HEIGHT -gt $BLOCK_HEIGHT_REACHED ]
do
    BLOCK_HEIGHT_REACHED=$(curl -X POST $RPC_LOTUS -H 'Content-Type: application/json' --data '{"jsonrpc":"2.0","id":1,"method":"Filecoin.ChainHead","params":[]}' | jq '.result.Height')
    echo "Workload [main][first.sh]: block height check: reached ${BLOCK_HEIGHT_REACHED}"
    if [ $INIT_BLOCK_HEIGHT -le $BLOCK_HEIGHT_REACHED ]; then
        break
    fi
    sleep 5
done
echo "Workload [main][first.sh]: chainhead has reached block height ${INIT_BLOCK_HEIGHT}"

echo "Workload [main][first.sh]: initializing wallets..."

python3 -u /opt/antithesis/resources/initialize_wallets.py "forest"
/opt/antithesis/app -node=Lotus1 -config=/opt/antithesis/resources/config.json -wallets=5 -operation=create
/opt/antithesis/app -node=Lotus2 -config=/opt/antithesis/resources/config.json -wallets=5 -operation=create

echo "Workload [main][first.sh]: completed workload setup."
