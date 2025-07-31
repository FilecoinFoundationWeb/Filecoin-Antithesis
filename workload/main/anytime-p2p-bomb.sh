#!/bin/bash

# Source health check functions
source "$(dirname "$0")/health_check_functions.sh"

# Perform health check before proceeding
log_script_start

echo "Starting anytime P2P bomb"

# Run the P2P bomb
/opt/antithesis/app stress p2p-bomb --node "Lotus1"

echo "P2P bomb completed"