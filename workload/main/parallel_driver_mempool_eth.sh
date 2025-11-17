#!/bin/bash

export FILECOIN_RPC="http://lotus-1:1234/rpc/v1" 
export FILECOIN_TOKEN=$(cat /root/devgen/lotus-1/jwt)

# Get default wallet addresses (or use environment variables if set)
FROM_ADDRESS="${FROM_ADDRESS:-$(filwizard wallet list | head -2 | tail -1 | awk '{print $1}')}"
TO_ADDRESS="${TO_ADDRESS:-$(filwizard wallet list | head -3 | tail -1 | awk '{print $1}')}"

if [ -z "$FROM_ADDRESS" ] || [ -z "$TO_ADDRESS" ]; then
    echo "ERROR: Could not determine FROM and TO addresses. Set FROM_ADDRESS and TO_ADDRESS environment variables."
    exit 1
fi

# Configurable via environment variables with defaults
VALUE="${VALUE:-1.0}"
DATA="${DATA:-}"
GAS_LIMIT="${GAS_LIMIT:-21000}"
MAX_FEE="${MAX_FEE:-1000000000}"
MAX_PRIORITY_FEE="${MAX_PRIORITY_FEE:-1000000000}"

# Build command with optional data flag
CMD="filwizard mempool eth --from $FROM_ADDRESS --to $TO_ADDRESS --value $VALUE --gas-limit $GAS_LIMIT --max-fee $MAX_FEE --max-priority-fee $MAX_PRIORITY_FEE --wait"

if [ -n "$DATA" ]; then
    CMD="$CMD --data $DATA"
fi

eval "$CMD"

