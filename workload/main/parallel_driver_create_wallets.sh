#!/bin/bash

# Source health check functions
source "$(dirname "$0")/health_check_functions.sh"

# Perform health check before proceeding
log_script_start

# Configurable parameters
APP_BINARY="/opt/antithesis/app"
CONFIG_FILE="/opt/antithesis/resources/config.json"
NODE_NAMES=("Lotus1" "Lotus2" "Forest")  
MIN_WALLETS=5                 
MAX_WALLETS=15                 

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

echo "Creating $random_wallet_count wallets on random node: $random_node"
for i in "${NODE_NAMES[@]}"; do
    $APP_BINARY wallet create --node "$i" --count "$random_wallet_count"
done

# Check if the last command succeeded
if [ $? -eq 0 ]; then
    echo "Successfully created $random_wallet_count wallets on $random_node"
else
    echo "Failed to create wallets on $random_node."
fi

echo "Wallet creation workload completed."

