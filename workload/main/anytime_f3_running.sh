#!/bin/bash

# Source health check functions
source "$(dirname "$0")/health_check_functions.sh"

# Perform health check before proceeding
log_script_start

echo "Checking F3IsRunning status for all nodes"

/opt/antithesis/app monitor f3