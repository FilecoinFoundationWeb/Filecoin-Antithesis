#!/bin/bash

# Define array of node URLs
NODE_URLS=(
    "http://lotus-1:1234/rpc/v2"
    "http://lotus-2:1235/rpc/v2"
)

# Select random node URL
RANDOM_NODE_URL=${NODE_URLS[$RANDOM % ${#NODE_URLS[@]}]}

echo "Testing RPC endpoint: $RANDOM_NODE_URL"
/opt/antithesis/app -operation rpc-benchmark -rpc-url "$RANDOM_NODE_URL" 