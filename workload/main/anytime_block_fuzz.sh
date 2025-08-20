#!/bin/bash

# Source health check functions
source "$(dirname "$0")/health_check_functions.sh"

# Perform health check before proceeding
log_script_start

echo "Starting parallel block fuzzing"


NODES=("Lotus1" "Lotus2" "Forest")

for NODE in "${NODES[@]}"; do
    log_info "Running block fuzzing on ${NODE}"
    /opt/antithesis/app stress block-fuzz --node ${NODE}
done

echo "Parallel block fuzzing completed" 