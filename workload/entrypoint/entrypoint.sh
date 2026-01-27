#!/bin/bash
#
# Workload Entrypoint Script
# ==========================
# This script initializes the workload container by:
# 1. Synchronizing system time
# 2. Waiting for the blockchain to reach a minimum height
# 3. Deploying smart contracts via FilWizard
# 4. Extracting and sharing contract addresses for other containers
# 5. Creating environment files for Curio
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
RPC_LOTUS="${RPC_LOTUS:-http://lotus0:1234/rpc/v0}"
FILECOIN_RPC="http://lotus0:1234/rpc/v1"
ETH_RPC_URL="http://lotus0:1234/rpc/v1"

# File paths
WORKSPACE_PATH="/opt/antithesis/FilWizard/workspace"
DEPLOYMENTS_FILE="/root/devgen/deployments.json"
SERVICE_CONTRACTS_DEPLOYMENTS="/opt/antithesis/FilWizard/workspace/filecoinwarmstorage/service_contracts/deployments.json"
WORKSPACE_DEPLOYMENTS="/opt/antithesis/FilWizard/workspace/deployments.json"
CURIO_SHARED_DIR="/root/devgen/curio"
CURIO_ENV_FILE="${CURIO_SHARED_DIR}/.env.curio"

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

wait_for_file() {
    local file="$1"
    local description="$2"
    log_info "Waiting for $description..."
    while [ ! -f "$file" ]; do
        sleep 2
    done
    log_info "$description found"
}

# =============================================================================
# WAIT FOR BLOCKCHAIN
# =============================================================================

log_info "Waiting for block height to reach ${INIT_BLOCK_HEIGHT}..."

BLOCK_HEIGHT_REACHED=0
while [ $INIT_BLOCK_HEIGHT -gt $BLOCK_HEIGHT_REACHED ]; do
    BLOCK_HEIGHT_REACHED=$(curl -s -X POST "$RPC_LOTUS" \
        -H 'Content-Type: application/json' \
        --data '{"jsonrpc":"2.0","id":1,"method":"Filecoin.ChainHead","params":[]}' \
        | jq '.result.Height')
    
    if [ $INIT_BLOCK_HEIGHT -le $BLOCK_HEIGHT_REACHED ]; then
        break
    fi
    log_info "Current height: ${BLOCK_HEIGHT_REACHED}, waiting..."
    sleep 5
done

log_info "Blockchain ready at height ${BLOCK_HEIGHT_REACHED}"

# =============================================================================
# SETUP ENVIRONMENT
# =============================================================================

export FILECOIN_RPC
export ETH_RPC_URL
export FILECOIN_TOKEN=$(cat "$LOTUS_0_DATA_DIR/lotus0-jwt")

log_info "Environment configured:"
log_info "  FILECOIN_RPC: $FILECOIN_RPC"
log_info "  LOTUS_0_DATA_DIR: $LOTUS_0_DATA_DIR"

# =============================================================================
# DEPLOY CONTRACTS
# =============================================================================

log_info "Deploying smart contracts via FilWizard..."

cd /opt/antithesis/FilWizard

filwizard contract deploy-local \
    --config /opt/antithesis/FilWizard/config/filecoin-synapse.json \
    --workspace ./workspace \
    --rpc-url "$FILECOIN_RPC" \
    --create-deployer \
    --bindings \
    || log_warn "FilWizard deployment completed with warnings/errors, continuing..."

wait_for_file "$WORKSPACE_DEPLOYMENTS" "deployments.json"

# =============================================================================
# EXTRACT CONTRACT ADDRESSES
# =============================================================================

log_info "Extracting contract addresses..."

# Create shared directory
mkdir -p "$(dirname "$DEPLOYMENTS_FILE")"

# Extract service contract addresses (flat object format)
jq '.["31415926"] | {
  PDP_VERIFIER_IMPLEMENTATION_ADDRESS,
  PDP_VERIFIER_PROXY_ADDRESS,
  FILECOIN_PAY_ADDRESS,
  SERVICE_PROVIDER_REGISTRY_IMPLEMENTATION_ADDRESS,
  SERVICE_PROVIDER_REGISTRY_PROXY_ADDRESS,
  SIGNATURE_VERIFICATION_LIB_ADDRESS,
  FWSS_IMPLEMENTATION_ADDRESS,
  FWSS_PROXY_ADDRESS,
  FWSS_VIEW_ADDRESS
}' "$SERVICE_CONTRACTS_DEPLOYMENTS" > "$DEPLOYMENTS_FILE"

# Parse service contract addresses
PDP_VERIFIER_ADDRESS=$(jq -r '.PDP_VERIFIER_PROXY_ADDRESS // empty' "$DEPLOYMENTS_FILE")
FWSS_ADDRESS=$(jq -r '.FWSS_PROXY_ADDRESS // empty' "$DEPLOYMENTS_FILE")
SP_REGISTRY_ADDRESS=$(jq -r '.SERVICE_PROVIDER_REGISTRY_PROXY_ADDRESS // empty' "$DEPLOYMENTS_FILE")
PAYMENTS_ADDRESS=$(jq -r '.FILECOIN_PAY_ADDRESS // empty' "$DEPLOYMENTS_FILE")
FWSS_VIEW_ADDRESS=$(jq -r '.FWSS_VIEW_ADDRESS // empty' "$DEPLOYMENTS_FILE")

# Extract USDFC and Multicall3 from workspace deployments (array format)
USDFC_ADDRESS=$(jq -r '.[] | select(.name=="usdfc") | .address' "$WORKSPACE_DEPLOYMENTS")
MULTICALL3_ADDRESS=$(jq -r '.[] | select(.name=="Multicall3") | .address' "$WORKSPACE_DEPLOYMENTS")

# Merge all addresses into shared deployments file
jq --arg usdfc "$USDFC_ADDRESS" --arg multicall "$MULTICALL3_ADDRESS" \
    '. + {USDFC_ADDRESS: $usdfc, MULTICALL3_ADDRESS: $multicall}' \
    "$DEPLOYMENTS_FILE" > "${DEPLOYMENTS_FILE}.tmp" \
    && mv "${DEPLOYMENTS_FILE}.tmp" "$DEPLOYMENTS_FILE"

log_info "Contract addresses extracted:"
log_info "  PDP Verifier:     $PDP_VERIFIER_ADDRESS"
log_info "  FWSS (Proxy):     $FWSS_ADDRESS"
log_info "  FWSS View:        $FWSS_VIEW_ADDRESS"
log_info "  SP Registry:      $SP_REGISTRY_ADDRESS"
log_info "  Payments:         $PAYMENTS_ADDRESS"
log_info "  USDFC:            $USDFC_ADDRESS"
log_info "  Multicall3:       $MULTICALL3_ADDRESS"

# =============================================================================
# CREATE CURIO ENVIRONMENT FILE
# =============================================================================

log_info "Creating Curio environment file..."

mkdir -p "$CURIO_SHARED_DIR"

cat > "$CURIO_ENV_FILE" << EOF
# Curio Devnet Contract Addresses
# Generated by workload entrypoint.sh

CURIO_DEVNET_PDP_VERIFIER_ADDRESS=$PDP_VERIFIER_ADDRESS
CURIO_DEVNET_FWSS_ADDRESS=$FWSS_ADDRESS
CURIO_DEVNET_SERVICE_REGISTRY_ADDRESS=$SP_REGISTRY_ADDRESS
CURIO_DEVNET_PAYMENTS_ADDRESS=$PAYMENTS_ADDRESS
CURIO_DEVNET_USDFC_ADDRESS=$USDFC_ADDRESS
CURIO_DEVNET_MULTICALL_ADDRESS=$MULTICALL3_ADDRESS
EOF

log_info "Curio env file created: $CURIO_ENV_FILE"

# =============================================================================
# RUN WORKLOAD
# =============================================================================

log_info "Contract deployment and environment setup complete!"
log_info "Starting workload main process..."
/opt/antithesis/entrypoint/setup-synapse.sh
python3 -u /opt/antithesis/entrypoint/setup_complete.py

# Keep container running
sleep infinity
