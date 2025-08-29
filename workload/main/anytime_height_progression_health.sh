#!/bin/bash

# Source health check functions
source "$(dirname "$0")/health_check_functions.sh"

# Perform health check before proceeding
log_script_start

echo "Running height progression monitoring for all nodes"

/opt/antithesis/app monitor height-progression --duration 2m --interval 8s --max-stalls 10
