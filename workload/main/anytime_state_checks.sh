#!/bin/bash

# Source health check functions
source "$(dirname "$0")/health_check_functions.sh"

# Skip if reorg singleton is running (avoid health checks during reorg)
exit_if_reorg_running

# Perform health check before proceeding
log_script_start

# Array of available nodes
NODES=("Lotus1" "Lotus2" "Forest")

# Loop through all nodes and check state
for node in "${NODES[@]}"; do
    echo "Running state mismatch check for node: $node"
    /opt/antithesis/app state check --node "$node"
done

exit 0
