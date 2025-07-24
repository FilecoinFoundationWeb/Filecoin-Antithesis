#!/bin/bash
set -e

echo "Starting reorg simulation"
# Call the workload CLI reorg command
/opt/antithesis/app network reorg --node "$NODE_NAME" --check-consensus