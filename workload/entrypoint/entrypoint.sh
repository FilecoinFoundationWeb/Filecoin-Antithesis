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

RPC_LOTUS="${RPC_LOTUS:-http://10.20.20.26:1235/rpc/v0}"

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

/opt/antithesis/entrypoint/deploy-contracts.sh

python3 -u /opt/antithesis/entrypoint/setup_complete.py

sleep infinity