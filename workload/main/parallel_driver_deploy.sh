#!/bin/bash

APP_BINARY="/opt/antithesis/app"
CONFIG_FILE="/opt/antithesis/resources/config.json"
OPERATION="deploySimpleCoin"
NODE_NAMES=("Lotus1")
CONTRACT_FILE="/opt/antithesis/resources/smart-contracts/SimpleCoin.hex"

# Ensure the application binary exists
if [ ! -f "$APP_BINARY" ]; then
    echo "Error: $APP_BINARY not found."
    exit 1
fi

# Ensure the configuration file exists
if [ ! -f "$CONFIG_FILE" ]; then
    echo "Error: $CONFIG_FILE not found."
    exit 1
fi

# Ensure the contract file exists
if [ ! -f "$CONTRACT_FILE" ]; then
    echo "Error: Contract file $CONTRACT_FILE not found."
    exit 1
fi

# Function to pick a random node
select_random_node() {
    local index=$((RANDOM % ${#NODE_NAMES[@]}))
    echo "${NODE_NAMES[$index]}"
}

random_node=$(select_random_node)

echo "Deploying smart contract $CONTRACT_FILE on node $random_node"

# Execute the deployment operation
$APP_BINARY --operation "$OPERATION" --node "$random_node" --contract "$CONTRACT_FILE" --config "$CONFIG_FILE"
if [ $? -ne 0 ]; then
    echo "Error: Deployment failed."
    exit 1
fi

echo "Smart contract $CONTRACT_FILE successfully deployed on node $random_node."
