#!/bin/bash

# Source health check functions
source "$(dirname "$0")/health_check_functions.sh"

# Perform health check before proceeding
log_script_start

# Array of available nodes
NODES=("Lotus1" "Lotus2")

# Function to get a random node
get_random_node() {
    local len=${#NODES[@]}
    local rand_index=$((RANDOM % len))
    echo "${NODES[$rand_index]}"
}

# Get a random node
SELECTED_NODE=$(get_random_node)

echo "Selected node for state mismatch check: $SELECTED_NODE"

# Run state mismatch check
/opt/antithesis/app state check --node "$SELECTED_NODE"
