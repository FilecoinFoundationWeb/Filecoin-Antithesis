#!/bin/bash
set -e

echo "Starting reorg simulation"
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

echo "Selected node for reorg simulation: $SELECTED_NODE"

# Call the workload CLI reorg command
/opt/antithesis/app network reorg --node "$SELECTED_NODE" --check-consensus