#!/bin/bash

export FILECOIN_RPC="http://lotus-1:1234/rpc/v1"

# Get default wallet addresses (or use environment variables if set)
FROM_ADDRESS="${FROM_ADDRESS:-$(filwizard wallet list | head -2 | tail -1 | awk '{print $1}')}"
TO_ADDRESS="${TO_ADDRESS:-$(filwizard wallet list | head -3 | tail -1 | awk '{print $1}')}"
AMOUNT="${AMOUNT:-0.1}"

if [ -z "$FROM_ADDRESS" ] || [ -z "$TO_ADDRESS" ]; then
    echo "ERROR: Could not determine FROM and TO addresses. Set FROM_ADDRESS and TO_ADDRESS environment variables."
    exit 1
fi

filwizard mempool send "$FROM_ADDRESS" "$TO_ADDRESS" "$AMOUNT" --wait

