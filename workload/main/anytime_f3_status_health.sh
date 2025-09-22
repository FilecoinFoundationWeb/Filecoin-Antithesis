#!/bin/bash

# Source health check functions
source "$(dirname "$0")/health_check_functions.sh"

# Perform health check before proceeding
log_script_start

echo "Running F3 status check for all nodes"

/opt/antithesis/app monitor f3-status
