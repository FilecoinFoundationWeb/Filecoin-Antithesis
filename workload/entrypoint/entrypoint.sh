#!/bin/bash

#set -e

# What is the purpose of the following?
# Adjusting the Antithesis system time is not a permitted operation
echo "synchronizing system time..."
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
INIT_BLOCK_HEIGHT="${INIT_BLOCK_HEIGHT:-5}"
BLOCK_HEIGHT_REACHED=0

echo "waiting for block height to reach ${INIT_BLOCK_HEIGHT}"

while [ $INIT_BLOCK_HEIGHT -gt $BLOCK_HEIGHT_REACHED ]
do

    # Capture response separately from exit code
    response=$(curl -s --fail -X POST "$RPC_LOTUS" \
        -H 'Content-Type: application/json' \
        --data '{"jsonrpc":"2.0","id":1,"method":"Filecoin.ChainHead","params":[]}' 2>/dev/null)
    curl_exit=$?
    
    # If curl failed, retry
    if [[ $curl_exit -ne 0 ]]; then
        echo "lotus0 not available yet, retrying..."
        sleep 5
        continue
    fi
    
    # Parse the response
    BLOCK_HEIGHT_REACHED=$(echo "$response" | jq -r '.result.Height // 0')

    echo "current height: $BLOCK_HEIGHT_REACHED"

    if [ $INIT_BLOCK_HEIGHT -le $BLOCK_HEIGHT_REACHED ]; then
        break
    fi
    sleep 5
done
cd /opt/antithesis/filwizard
./filwizard contract clone-config --config config/filecoin-synapse.json --workspace ./workspace
echo "Workload [entrypoint]: chainhead has reached block height ${INIT_BLOCK_HEIGHT}"

python3 -u /opt/antithesis/entrypoint/setup_complete.py

sleep infinity