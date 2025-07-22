#!/bin/bash

echo "Starting parallel block fuzzing"


# Randomly select a node
if [ $((RANDOM % 2)) -eq 0 ]; then
    NODE="Lotus1"
else
    NODE="Lotus2"
fi

log_info "Running block fuzzing on ${NODE}"

# Run block fuzzing
/opt/antithesis/app -operation blockfuzz -node "${NODE}"

echo "Parallel block fuzzing completed" 