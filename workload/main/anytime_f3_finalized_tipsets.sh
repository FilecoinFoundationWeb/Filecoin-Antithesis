#!/bin/bash

# Source health check functions
source "$(dirname "$0")/health_check_functions.sh"

# Skip if reorg singleton is running (avoid health checks during reorg)
exit_if_reorg_running

# Perform health check before proceeding
log_script_start

echo "Asserting finalized tipsets are the same between two nodes"

/opt/antithesis/app consensus finalized