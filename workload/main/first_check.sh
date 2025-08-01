#!/bin/bash

set -e

# Source health check functions
source "$(dirname "$0")/health_check_functions.sh"

# Perform health check before proceeding
log_script_start

echo "Workload [main][first.sh]: initializing wallets..."

python3 -u /opt/antithesis/resources/initialize_wallets.py "forest"

echo "Workload [main][first.sh]: completed workload setup."
