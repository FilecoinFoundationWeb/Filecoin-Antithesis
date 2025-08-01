#!/bin/bash

# Source health check functions
source "$(dirname "$0")/health_check_functions.sh"

# Perform health check before proceeding
log_script_start

echo "Asserting finalized tipsets are the same between two nodes"

/opt/antithesis/app consensus finalized