#!/bin/bash

# Source health check functions
source "$(dirname "$0")/health_check_functions.sh"

# Perform health check before proceeding
log_script_start

echo "Running ETH methods consistency check"
# Run the check with a random height
/opt/antithesis/app eth check