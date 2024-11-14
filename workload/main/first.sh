#!/bin/bash

set -e

RPC_LOTUS_1="${RPC_LOTUS:-http://10.20.20.24:1234/rpc/v0}"
RPC_LOTUS_2="${RPC_LOTUS:-http://10.20.20.26:1235/rpc/v0}"
RPC_FOREST="${RPC_FOREST:-http://10.20.20.28:3456/rpc/v0}"

# Waiting for lotus 1 rpc endpoint to come online
RPC_READY=0
while [ $RPC_READY -eq 0 ]
do
    echo "Workload [main][first.sh]: waiting for lotus1 RPC endpoint (chainhead at ${RPC_LOTUS_1}) to come online..."
    RPC_READY=$(curl -X POST $RPC_LOTUS_1 -H 'Content-Type: application/json' --data '{"jsonrpc":"2.0","id":1,"method":"Filecoin.ChainHead","params":[]}' | grep result | wc -l)
    if [ $RPC_READY -eq 0 ]; then
        break
    fi
    sleep 5
done
echo "Workload [main][first.sh]: chainhead RPC endpoint online from lotus1"


# Waiting for lotus 2 rpc endpoint to come online
RPC_READY=0
while [ $RPC_READY -eq 0 ]
do
    echo "Workload [main][first.sh]: waiting for lotus2 RPC endpoint (chainhead at ${RPC_LOTUS_2}) to come online..."
    RPC_READY=$(curl -X POST $RPC_LOTUS_2 -H 'Content-Type: application/json' --data '{"jsonrpc":"2.0","id":1,"method":"Filecoin.ChainHead","params":[]}' | grep result | wc -l)
    if [ $RPC_READY -eq 0 ]; then
        break
    fi
    sleep 5
done
echo "Workload [main][first.sh]: chainhead RPC endpoint online from lotus2"


# Waiting for forest rpc endpoint to come online
RPC_READY=0
while [ $RPC_READY -eq 0 ]
do
    echo "Workload [main][first.sh]: waiting for forest RPC endpoint (chainhead at ${RPC_FOREST}) to come online..."
    RPC_READY=$(curl -X POST $RPC_FOREST -H 'Content-Type: application/json' --data '{"jsonrpc":"2.0","id":1,"method":"Filecoin.ChainHead","params":[]}' | grep result | wc -l)
    if [ $RPC_READY -eq 0 ]; then
        break
    fi
    sleep 5
done
echo "Workload [main][first.sh]: chainhead RPC endpoint online from forest"


# Waiting for the chain head to pass a certain height
INIT_BLOCK_HEIGHT="${INIT_BLOCK_HEIGHT:-15}"
BLOCK_HEIGHT_REACHED=0
echo "Workload [main][first.sh]: waiting for block height to reach ${INIT_BLOCK_HEIGHT}"
while [ $INIT_BLOCK_HEIGHT -gt $BLOCK_HEIGHT_REACHED ]
do
    BLOCK_HEIGHT_REACHED=$(curl -X POST $RPC_LOTUS_1 -H 'Content-Type: application/json' --data '{"jsonrpc":"2.0","id":1,"method":"Filecoin.ChainHead","params":[]}' | jq '.result.Height')
    echo "Workload [main][first.sh]: block height check: reached ${BLOCK_HEIGHT_REACHED}"
    if [ $INIT_BLOCK_HEIGHT -le $BLOCK_HEIGHT_REACHED ]; then
        break
    fi
    sleep 5
done
echo "Workload [main][first.sh]: chainhead has reached block height ${INIT_BLOCK_HEIGHT}"

echo "Workload [main][first.sh]: initializing wallets..."
python3 -u /opt/antithesis/resources/initialize_wallets.py "lotus-1"
python3 -u /opt/antithesis/resources/initialize_wallets.py "lotus-2"
python3 -u /opt/antithesis/resources/initialize_wallets.py "forest"

echo "Workload [main][first.sh]: completed workload setup."
