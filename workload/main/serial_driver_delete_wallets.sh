#!/bin/bash

# Source health check functions
source "$(dirname "$0")/health_check_functions.sh"

# Perform health check before proceeding
log_script_start

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

select_random_node() {
    local index=$((RANDOM % ${#NODE_NAMES[@]}))
    echo "${NODE_NAMES[$index]}"
}

random_node=$(select_random_node)

echo "Deleting wallets on $random_node"
$APP_BINARY wallet delete --node "$random_node"