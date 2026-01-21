#!/bin/bash

WORKLOAD="/opt/antithesis/workload"

# Array of available nodes (must match config.json)
NODES=("Lotus0" "Lotus1" "Forest0")

# Loop through all nodes and check state
for node in "${NODES[@]}"; do
    echo "Running state check for node: $node"
    $WORKLOAD state check --node "$node"
done

# Also run cross-node state comparison
echo "Running cross-node state comparison..."
$WORKLOAD state compare --epochs 10

exit 0
