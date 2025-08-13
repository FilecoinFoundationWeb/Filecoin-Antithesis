#!/bin/bash

# Source health check functions
source "$(dirname "$0")/health_check_functions.sh"

# Perform health check before proceeding
log_script_start

echo "Running Workload: Chain Backfill"
/opt/antithesis/app chain backfill 

echo "Checking peers"
/opt/antithesis/app monitor peers


echo "Checking F3IsRunning status for all nodes"
/opt/antithesis/app monitor f3

echo "Asserting finalized tipsets are the same between two nodes"
/opt/antithesis/app consensus finalized

echo "Running ETH methods consistency check"
/opt/antithesis/app eth check

echo "Checking state mismatch between two nodes"
/opt/antithesis/app state check --node "Lotus1" --window 50
/opt/antithesis/app state check --node "Lotus2" --window 50
