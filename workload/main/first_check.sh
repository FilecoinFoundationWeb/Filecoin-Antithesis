#!/bin/bash

set -e

# Source health check functions
source "$(dirname "$0")/health_check_functions.sh"

# Perform health check before proceeding
log_script_start

echo "Workload [main][first_check.sh]: completed workload setup."
