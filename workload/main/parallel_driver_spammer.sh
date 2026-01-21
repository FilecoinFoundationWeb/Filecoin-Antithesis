#!/bin/bash

WORKLOAD="/opt/antithesis/workload"
CONFIG_FILE="/opt/antithesis/resources/config.json"
NODE_NAMES=("Lotus0" "Lotus1" "Forest0")

if [ ! -f "$WORKLOAD" ]; then
    echo "Error: $WORKLOAD not found."
    exit 1
fi

if [ ! -f "$CONFIG_FILE" ]; then
    echo "Error: $CONFIG_FILE not found."
    exit 1
fi

# First try running spam without creating new wallets
echo "Attempting to spam transactions with existing wallets..."
$WORKLOAD mempool spam

# If spam failed, create wallets with higher funding
if [ $? -ne 0 ]; then
    echo "Spam operation failed. Creating new well-funded wallets..."
    
    # Create wallets on all nodes
    for node in "${NODE_NAMES[@]}"; do
        echo "Creating well-funded wallets on $node..."
        $WORKLOAD wallet create --node "$node" --count 3
        sleep 5
    done
    
    echo "Waiting for wallet creation to finalize..."
    sleep 10
    
    echo "Retrying spam operation with new wallets..."
    $WORKLOAD mempool spam
fi
