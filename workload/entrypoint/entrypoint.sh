#!/bin/bash

set -e

echo "Workload [entrypoint]: synchronizing system time..."
# Attempt to sync time with NTP server
if ntpdate -q pool.ntp.org &>/dev/null; then
    # If query works, try to sync
    ntpdate -u pool.ntp.org || {
        echo "Warning: Time sync failed. If running in a container, it may need the SYS_TIME capability."
        echo "Run the container with: --cap-add SYS_TIME"
    }
else
    echo "Warning: Unable to query NTP servers. Check network connectivity."
fi

current_time=$(date -u "+%Y-%m-%d %H:%M:%S UTC")
echo "Current system time: $current_time"

RPC_LOTUS="${RPC_LOTUS:-http://lotus0:1234/rpc/v0}"

# Waiting for the chain head to pass a certain height
INIT_BLOCK_HEIGHT=5
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
export FILECOIN_RPC="http://lotus0:1234/rpc/v1"
echo LOTUS_0_DATA_DIR=$LOTUS_0_DATA_DIR
export FILECOIN_TOKEN=$(cat $LOTUS_0_DATA_DIR/lotus0-jwt)
#echo FILECOIN_TOKEN=$FILECOIN_TOKEN
export ETH_RPC_URL="http://lotus0:1234/rpc/v1"
pwd
filwizard contract deploy-local --config /opt/antithesis/FilWizard/config/filecoin-synapse.json --workspace ./workspace --rpc-url "$FILECOIN_RPC" --create-deployer --bindings || echo "Filwizard deployment completed with warnings/errors, but continuing..."

# Wait for deployments.json to be created (either by filwizard or other deployment process)
echo "Waiting for deployments.json to be created..."
while [ ! -f ./workspace/deployments.json ]; do
    echo "Waiting for deployments.json..."
    sleep 2
done

# Copy full deployments.json to shared volume and extract PDP verifier address
echo "Copying deployments.json to shared volume..."
cp ./workspace/deployments.json /root/devgen/deployments.json
echo "Workload [entrypoint]: Copied full deployments.json to /root/devgen/"

echo "Extracting PDP verifier address from deployments.json..."
PDP_VERIFIER_ADDRESS=$(cat ./workspace/deployments.json | jq -r '.[] | select(.name=="pdpverifier") | .address')

if [ -n "$PDP_VERIFIER_ADDRESS" ] && [ "$PDP_VERIFIER_ADDRESS" != "null" ]; then
    echo "PDP_VERIFIER_ADDRESS=$PDP_VERIFIER_ADDRESS" > /root/devgen/curio/pdp_contract_address.env
    echo "Workload [entrypoint]: PDP Verifier deployed at: $PDP_VERIFIER_ADDRESS"
    echo "Workload [entrypoint]: Created PDP contract address file successfully"
    echo "Workload [entrypoint]: Full deployments.json available at /root/devgen/deployments.json"
else
    echo "Workload [entrypoint]: ERROR - Could not extract PDP verifier address from deployments.json"
    cat ./workspace/deployments.json | jq '.'
fi

# # Call setup-synapse.sh to configure synapse SDK
# echo "Workload [entrypoint]: Setting up synapse SDK..."
/opt/antithesis/entrypoint/setup-synapse.sh

python3 -u /opt/antithesis/entrypoint/setup_complete.py
sleep infinity