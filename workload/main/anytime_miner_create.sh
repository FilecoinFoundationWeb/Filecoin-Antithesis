#!/bin/bash

# Source common functions
source /opt/antithesis/test/v1/main/health_check_functions.sh

# List of test nodes
NODES=("Lotus1" "Lotus2")

# List of deposit test scenarios
DEPOSIT_TESTS=("normal" "zero" "negative" "excess")

# Function to get a random node
get_random_node() {
    local random_index=$((RANDOM % ${#NODES[@]}))
    echo "${NODES[$random_index]}"
}

# Function to get a random deposit test
get_random_deposit_test() {
    local random_index=$((RANDOM % ${#DEPOSIT_TESTS[@]}))
    echo "${DEPOSIT_TESTS[$random_index]}"
}

# Main test loop
for i in {1..4}; do
    NODE=$(get_random_node)
    TEST=$(get_random_deposit_test)
    
    echo "=== Test $i ==="
    echo "Node: $NODE"
    echo "Deposit Test: $TEST"
    
    # Run the miner creation test
    /opt/antithesis/app miner create \
        --node "$NODE" \
        --deposit-test "$TEST"
    
    # Wait between tests to allow chain to settle
    echo "Waiting for chain to settle..."
    sleep 60
done
