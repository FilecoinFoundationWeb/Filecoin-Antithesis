#!/bin/bash

# Description: Run chain synchronization test between two Lotus nodes
# This script will run the chain sync test and verify that both nodes have the same chain state

echo "Running Test: Chain Sync Test"
go test -v -count=1 /opt/antithesis/go-test-scripts/chain_sync_test.go

# Check the exit status
if [ $? -eq 0 ]; then
    echo "Chain sync test passed successfully"
else
    echo "Chain sync test failed"
    exit 1
fi 