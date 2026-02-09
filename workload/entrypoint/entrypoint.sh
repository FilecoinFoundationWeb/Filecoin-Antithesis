#!/bin/bash
#
# Workload Entrypoint Script
# ==========================
# This script initializes the workload container by:
# 0. Generating pre-funded genesis wallets
# 1. Synchronizing system time
# 2. Waiting for the blockchain to reach a minimum height
# 3. Launching the stress engine
#

set -e

# =============================================================================
# CONFIGURATION
# =============================================================================

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

# RPC endpoints
RPC_LOTUS="${RPC_LOTUS:-http://lotus0:1234/rpc/v1}"

# Blockchain settings
INIT_BLOCK_HEIGHT=5

# =============================================================================
# HELPER FUNCTIONS
# =============================================================================

log_info() {
    echo -e "${GREEN}[ENTRYPOINT]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[ENTRYPOINT]${NC} $1"
}

log_error() {
    echo -e "${RED}[ENTRYPOINT]${NC} $1" >&2
}

# =============================================================================
# STEP 0: GENERATE GENESIS WALLETS
# =============================================================================

log_info "Generating pre-funded genesis wallets..."
/opt/antithesis/genesis-prep --count 100 --out /shared/configs
log_info "Genesis wallet generation complete."

# =============================================================================
# STEP 1: TIME SYNCHRONIZATION
# =============================================================================

log_info "Synchronizing system time..."

if ntpdate -q pool.ntp.org &>/dev/null; then
    ntpdate -u pool.ntp.org || {
        log_warn "Time sync failed. Container may need SYS_TIME capability."
        log_warn "Run with: --cap-add SYS_TIME"
    }
else
    log_warn "Unable to query NTP servers. Check network connectivity."
fi

log_info "Current system time: $(date -u '+%Y-%m-%d %H:%M:%S UTC')"

# =============================================================================
# STEP 2: WAIT FOR BLOCKCHAIN
# =============================================================================

log_info "Waiting for block height to reach ${INIT_BLOCK_HEIGHT}..."

BLOCK_HEIGHT_REACHED=0
while [ "${INIT_BLOCK_HEIGHT}" -gt "${BLOCK_HEIGHT_REACHED}" ]; do
    # Get height, default to 0 if curl fails or response is empty/null
    RESPONSE=$(curl -s --max-time 5 -X POST "$RPC_LOTUS" \
        -H 'Content-Type: application/json' \
        --data '{"jsonrpc":"2.0","id":1,"method":"Filecoin.ChainHead","params":[]}' 2>/dev/null || echo '{}')
    
    BLOCK_HEIGHT_REACHED=$(echo "$RESPONSE" | jq -r '.result.Height // 0' 2>/dev/null)
    
    # If jq failed or returned empty, set to 0
    if [ -z "$BLOCK_HEIGHT_REACHED" ] || [ "$BLOCK_HEIGHT_REACHED" = "null" ]; then
        BLOCK_HEIGHT_REACHED=0
    fi

    if [ "${INIT_BLOCK_HEIGHT}" -le "${BLOCK_HEIGHT_REACHED}" ]; then
        break
    fi
    log_info "Current height: ${BLOCK_HEIGHT_REACHED}, waiting..."
    sleep 5
done

log_info "Blockchain ready at height ${BLOCK_HEIGHT_REACHED}"

# =============================================================================
# STEPS 3-6: CONTRACT DEPLOYMENT (commented out — FilWizard/Synapse not active)
# =============================================================================

# if [ -f "/usr/local/bin/filwizard" ]; then
#     export FILECOIN_RPC="http://lotus0:1234/rpc/v1"
#     export ETH_RPC_URL="http://lotus0:1234/rpc/v1"
#     export FILECOIN_TOKEN=$(cat "$LOTUS_0_DATA_DIR/lotus0-jwt")
#
#     log_info "Deploying smart contracts via FilWizard..."
#     cd /opt/antithesis/FilWizard
#     filwizard contract deploy-local \
#         --config /opt/antithesis/FilWizard/config/filecoin-synapse.json \
#         --workspace ./workspace \
#         --rpc-url "$FILECOIN_RPC" \
#         --create-deployer \
#         --bindings \
#         || log_warn "FilWizard deployment completed with warnings/errors"
#
#     # Extract contract addresses, create curio env, run synapse setup...
#     # See git history for full implementation.
# fi

# =============================================================================
# STEP 7: LAUNCH STRESS ENGINE
# =============================================================================

log_info "Setup complete! Launching stress engine..."

# Signal to Antithesis that setup is complete
if [ -f "/opt/antithesis/entrypoint/setup_complete.py" ]; then
    python3 -u /opt/antithesis/entrypoint/setup_complete.py
fi

# Replace shell with stress engine (blocks forever)
exec /opt/antithesis/stress-engine
