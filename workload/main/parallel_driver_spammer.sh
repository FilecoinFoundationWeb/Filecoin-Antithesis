#!/bin/bash


APP_BINARY="/opt/antithesis/app"
CONFIG_FILE="/opt/antithesis/resources/config.json"
NODE_NAMES=("Lotus1" "Lotus2" "Forest")

if [ ! -f "$APP_BINARY" ]; then
    echo "Error: $APP_BINARY not found."
    exit 1
fi

if [ ! -f "$CONFIG_FILE" ]; then
    echo "Error: $CONFIG_FILE not found."
    exit 1
fi

# First try running spam without creating new wallets
echo "Attempting to spam transactions with existing wallets..."
$APP_BINARY mempool spam

# If spam failed, create wallets with higher funding
if [ $? -ne 0 ]; then
    echo "Spam operation failed. Creating new well-funded wallets..."
    
    # Create wallets on both nodes with much higher funding
    # The exact amount needed is hard to predict, but 1B should be plenty
    for node in "${NODE_NAMES[@]}"; do
        echo "Creating well-funded wallets on $node..."
        $APP_BINARY wallet create --node "$node" --count 2
        
        # Allow some time for wallet creation to complete
        sleep 5
    done
    
    # Allow time for blockchain to process transactions
    echo "Waiting for wallet creation to finalize..."
    sleep 10
    
    echo "Retrying spam operation with new wallets..."
    $APP_BINARY mempool spam
fi

