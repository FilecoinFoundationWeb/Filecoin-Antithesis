#!/bin/bash

APP_BINARY="/opt/antithesis/app"
CONFIG_FILE="/opt/antithesis/resources/config.json"
OPERATION="deploy"
NODE_NAMES=("Lotus1" "Lotus2")
CONTRACT_DIR="/opt/antithesis/resources/smart-contracts"

if [ ! -f "$APP_BINARY" ]; then
    echo "Error: $APP_BINARY not found."
    exit 1
fi

if [ ! -f "$CONFIG_FILE" ]; then
    echo "Error: $CONFIG_FILE not found."
    exit 1
fi

if [ ! -d "$CONTRACT_DIR" ]; then
    echo "Error: Contract directory $CONTRACT_DIR not found."
    exit 1
fi

# Function to pick a random node
select_random_node() {
    local index=$((RANDOM % ${#NODE_NAMES[@]}))
    echo "${NODE_NAMES[$index]}"
}

# Function to pick a random smart contract
select_random_contract() {
    local contracts=("$CONTRACT_DIR"/*.hex)
    if [ ${#contracts[@]} -eq 0 ]; then
        echo "Error: No smart contract files found in $CONTRACT_DIR."
        exit 1
    fi
    local index=$((RANDOM % ${#contracts[@]}))
    echo "${contracts[$index]}"
}

random_node=$(select_random_node)
random_contract=$(select_random_contract)

echo "Deploying smart contract $random_contract on $random_node"
$APP_BINARY -config=$CONFIG_FILE -operation=$OPERATION -node=$random_node -contract=$random_contract

