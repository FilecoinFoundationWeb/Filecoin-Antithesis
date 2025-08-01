#!/bin/bash

# Source health check functions
source "$(dirname "$0")/health_check_functions.sh"

# Perform health check before proceeding
log_script_start

echo "Running Workload: Chain Backfill"
/opt/antithesis/app chain backfill 