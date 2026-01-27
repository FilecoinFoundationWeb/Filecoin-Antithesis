#!/bin/bash
set -e

ENV_FILE="/opt/antithesis/synapse-sdk/.env.localnet"
source $ENV_FILE

# Export all vars from the env file for the workload commands
export LOCALNET_WARM_STORAGE_CONTRACT_ADDRESS
export LOCALNET_WARM_STORAGE_VIEW_ADDRESS  # Required for settle command (has getDataSet)
export LOCALNET_PAYMENTS_ADDRESS
export LOCALNET_PDP_VERIFIER_ADDRESS
export LOCALNET_RPC_URL
export LOCALNET_MULTICALL3_ADDRESS
export CLIENT_PRIVATE_KEY

EVENTS_FILE="/tmp/synapse-events.json"
E2E_OUTPUT="/tmp/e2e-output.txt"

echo "[Synapse E2E] Starting event monitor in background..."
# Increased duration to 120s to capture both piece upload and settlement
/opt/antithesis/FilWizard/filwizard synapse monitor --duration 120 --output "$EVENTS_FILE" &
MONITOR_PID=$!

# Give monitor time to start
sleep 2

echo "[Synapse E2E] Running storage e2e test..."
PRIVATE_KEY="$CLIENT_PRIVATE_KEY" \
RPC_URL="$LOCALNET_RPC_URL" \
WARM_STORAGE_ADDRESS="$LOCALNET_WARM_STORAGE_CONTRACT_ADDRESS" \
MULTICALL3_ADDRESS="$LOCALNET_MULTICALL3_ADDRESS" \
node --env-file="$ENV_FILE" \
    /opt/antithesis/synapse-sdk/utils/example-storage-e2e.js \
    /opt/antithesis/synapse-sdk/README.md 2>&1 | tee "$E2E_OUTPUT"

# Extract data set ID from output (looks for "Data set ID: X")
DATA_SET_ID=$(grep -oP 'Data set ID:\s*\K\d+' "$E2E_OUTPUT" | head -1)

if [ -z "$DATA_SET_ID" ]; then
    echo "[Synapse E2E] WARNING: Could not extract data set ID from e2e output"
    DATA_SET_ID=1
    echo "[Synapse E2E] Using default data set ID: $DATA_SET_ID"
fi

echo "[Synapse E2E] Extracted Data set ID: $DATA_SET_ID"

# Wait a few seconds for chain to settle
echo "[Synapse E2E] Waiting for chain to process..."
sleep 5

# Settle payment rails
echo "[Synapse E2E] Settling payment rails for data set $DATA_SET_ID..."
/opt/antithesis/FilWizard/filwizard synapse settle \
    --data-set-id "$DATA_SET_ID" \
    --warm-storage "$LOCALNET_WARM_STORAGE_VIEW_ADDRESS" \
    --payments "$LOCALNET_PAYMENTS_ADDRESS" \
    --rpc "$LOCALNET_RPC_URL" \
    --private-key "$CLIENT_PRIVATE_KEY" || echo "[Synapse E2E] Settlement failed (may need more time for payments to accrue)"

echo "[Synapse E2E] Waiting for monitor to finish..."
wait $MONITOR_PID || true

echo "[Synapse E2E] Emitting Antithesis assertions..."
/opt/antithesis/FilWizard/filwizard synapse assert --input "$EVENTS_FILE"

echo "[Synapse E2E] Summary:"
/opt/antithesis/FilWizard/filwizard synapse summary --input "$EVENTS_FILE"

# Cleanup
rm -f "$E2E_OUTPUT"

# Expected output should now show:
# ✓ No PDP faults
# ✓ Pieces were added
# ✓ Settlements occurred