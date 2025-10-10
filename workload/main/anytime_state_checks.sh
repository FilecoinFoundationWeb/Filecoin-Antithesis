#!/bin/bash


# Array of available nodes
NODES=("Lotus1" "Lotus2" "Forest")

# Loop through all nodes and check state
for node in "${NODES[@]}"; do
    echo "Running state mismatch check for node: $node"
    /opt/antithesis/app state check --node "$node"
done

exit 0
