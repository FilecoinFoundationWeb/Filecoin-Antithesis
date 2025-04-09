#!/bin/bash

APP_BINARY="/opt/antithesis/app"
CONFIG_FILE="/opt/antithesis/resources/config.json"
NODE_NAMES=("Lotus1" "Lotus2")
TX_COUNT=$((RANDOM % 50 + 75))  # Random count between 75-125

# List of operations to randomly select from
OPERATIONS=("spamInvalidMessages" "chainedInvalidTx")

# Function to pick a random node
select_random_node() {
    local index=$((RANDOM % ${#NODE_NAMES[@]}))
    echo "${NODE_NAMES[$index]}"
}

# Function to pick a random operation
select_random_operation() {
    local index=$((RANDOM % ${#OPERATIONS[@]}))
    echo "${OPERATIONS[$index]}"
}

if [ ! -f "$APP_BINARY" ]; then
    echo "Error: $APP_BINARY not found."
    exit 1
fi

if [ ! -f "$CONFIG_FILE" ]; then
    echo "Error: $CONFIG_FILE not found."
    exit 1
fi

# Select random target node and operation
random_node=$(select_random_node)
random_operation=$(select_random_operation)
# Execute the operation
echo "Running invalid transaction attack on node: $random_node"
$APP_BINARY -config=$CONFIG_FILE -operation=$random_operation -node=$random_node -count=$TX_COUNT

echo "Invalid transaction test completed."
