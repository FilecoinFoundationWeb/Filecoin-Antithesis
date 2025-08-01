#!/bin/bash

# Source health check functions
source "$(dirname "$0")/health_check_functions.sh"

# Perform health check before proceeding
log_script_start

echo "Starting parallel block fuzzing"


# Randomly select a node
if [ $((RANDOM % 2)) -eq 0 ]; then
    NODE="Lotus1"
else
    NODE="Lotus2"
fi

log_info "Running block fuzzing on ${NODE}"

# Run block fuzzing
/opt/antithesis/app stress block-fuzz --node ${NODE}

echo "Parallel block fuzzing completed" 