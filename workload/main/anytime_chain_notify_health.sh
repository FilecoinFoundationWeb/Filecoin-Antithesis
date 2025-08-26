#!/bin/bash

# Source health check functions
source "$(dirname "$0")/health_check_functions.sh"

# Perform health check before proceeding
log_script_start

echo "Running chain notify monitoring for all nodes"

/opt/antithesis/app monitor chain-notify --duration 2m
