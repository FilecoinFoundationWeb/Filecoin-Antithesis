#!/bin/bash

# Configurable parameters
APP_BINARY="/opt/antithesis/app"
CONFIG_FILE="/opt/antithesis/resources/config.json"
NODE_NAMES=("Lotus1" "Lotus2")  # Replace with your actual node names
MIN_WALLETS=5                  # Minimum number of wallets to create
MAX_WALLETS=15                 # Maximum number of wallets to create

# Ensure the app binary exists
if [ ! -f "$APP_BINARY" ]; then
    echo "Error: $APP_BINARY not found. Please ensure the binary is available."
    exit 1
fi

# Ensure the config file exists
if [ ! -f "$CONFIG_FILE" ]; then
    echo "Error: $CONFIG_FILE not found. Please ensure the config file is available."
    exit 1
fi

# Function to pick a random node
select_random_node() {
    local index=$((RANDOM % ${#NODE_NAMES[@]}))
    echo "${NODE_NAMES[$index]}"
}

# Create random wallets
random_wallet_count=$((RANDOM % (MAX_WALLETS - MIN_WALLETS + 1) + MIN_WALLETS))
random_node=$(select_random_node)

echo "Creating $random_wallet_count wallets on random node: $random_node"
$APP_BINARY wallet create --node "$random_node" --count "$random_wallet_count"

# Check if the last command succeeded
if [ $? -eq 0 ]; then
    echo "Successfully created $random_wallet_count wallets on $random_node"
else
    echo "Failed to create wallets on $random_node."
fi

echo "Wallet creation workload completed."

