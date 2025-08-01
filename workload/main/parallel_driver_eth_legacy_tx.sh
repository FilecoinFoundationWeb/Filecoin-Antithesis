#!/bin/bash

# Source health check functions
source "$(dirname "$0")/health_check_functions.sh"

# Perform health check before proceeding
log_script_start

# Function to run ETH legacy transaction test on a node
run_eth_legacy_tx() {
    local node=$1
    echo "Running ETH legacy transaction test on node: $node"
    
    # Run the test and capture output and exit status
    output=$(/opt/antithesis/app eth legacy-tx --node "$node" 2>&1)
    status=$?
    
    if [ $status -eq 0 ]; then
        echo "[SUCCESS] ETH legacy transaction test completed on $node"
        echo "$output"
    else
        echo "[FAIL] ETH legacy transaction test failed on $node"
        echo "Error output: $output"
    fi
    
    return $status
}

# Array of nodes to test
nodes=("Lotus1" "Lotus2")

# Run tests in parallel
pids=()
failed=0

for node in "${nodes[@]}"; do
    run_eth_legacy_tx "$node" &
    pids+=($!)
done

# Wait for all processes and collect exit statuses
for pid in "${pids[@]}"; do
    wait $pid
    status=$?
    if [ $status -ne 0 ]; then
        failed=1
    fi
done

# Exit with failure if any test failed
if [ $failed -eq 1 ]; then
    echo "One or more ETH legacy transaction tests failed"
    exit 1
fi

echo "All ETH legacy transaction tests completed successfully"
exit 0 