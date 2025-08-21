#!/bin/bash

# Source health check functions
source "$(dirname "$0")/health_check_functions.sh"

# Perform health check before proceeding
log_script_start

echo "Running comprehensive node health monitoring with all features enabled"

# Run comprehensive check with extended duration for better monitoring
/opt/antithesis/app monitor comprehensive \
    --chain-notify \
    --height-progression \
    --peer-count \
    --f3-status \
    --monitor-duration 2m \
    --height-check-interval 10s \
    --max-consecutive-stalls 4
