#!/bin/bash
#
# Synapse SDK Setup Script
# ========================
# This script sets up the Synapse SDK environment by:
# 1. Loading contract addresses from shared deployments
# 2. Creating client wallet and loading SP private key
# 3. Creating environment file for synapse-sdk
# 4. Minting tokens for client and SP
# 5. Registering service provider via post-deploy-setup.js
# 6. Running e2e storage tests
#
# Prerequisites:
#   - entrypoint.sh must have run first (deploys contracts)
#   - Curio must be running (provides SP private key)
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

# File paths
WORKSPACE_PATH="/opt/antithesis/FilWizard/workspace"
ACCOUNTS_FILE="/opt/antithesis/FilWizard/workspace/accounts.json"
ENV_FILE="/opt/antithesis/synapse-sdk/.env.localnet"
DEPLOYMENTS_FILE="/root/devgen/deployments.json"
CURIO_DATA_DIR="/root/devgen/curio"
SP_PRIVATE_KEY_FILE="${CURIO_DATA_DIR}/private_key"

# Network configuration
NETWORK="devnet"
CHAIN_ID="31415926"
RPC_URL="http://lotus0:1234/rpc/v1"
RPC_WS_URL="ws://lotus0:1234/rpc/v1"
SP_SERVICE_URL="${SP_SERVICE_URL:-http://curio:80}"

# Provider configuration
SP_NAME="${SP_NAME:-My Devnet Provider}"
SP_DESCRIPTION="${SP_DESCRIPTION:-Devnet provider for Warm Storage}"

# =============================================================================
# HELPER FUNCTIONS
# =============================================================================

log_info() {
    echo -e "${GREEN}[SYNAPSE]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[SYNAPSE]${NC} $1"
}

log_error() {
    echo -e "${RED}[SYNAPSE]${NC} $1" >&2
}

log_success() {
    echo -e "${GREEN}[SYNAPSE] ✓${NC} $1"
}

require_file() {
    local file="$1"
    local description="$2"
    if [ ! -f "$file" ]; then
        log_error "ERROR: $description not found at $file"
        exit 1
    fi
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

require_var() {
    local value="$1"
    local name="$2"
    if [ -z "$value" ]; then
        log_error "ERROR: Failed to extract $name"
        exit 1
    fi
}

# =============================================================================
# STEP 1: SETUP ENVIRONMENT
# =============================================================================

log_info "Starting Synapse SDK setup..."

# Export Filecoin environment
export FILECOIN_RPC="$RPC_URL"
export FILECOIN_TOKEN=$(cat "/root/devgen/lotus0/lotus0-jwt")

log_info "Environment:"
log_info "  Network: $NETWORK"
log_info "  RPC URL: $RPC_URL"
log_info "  LOTUS_0_DATA_DIR: $LOTUS_0_DATA_DIR"

# =============================================================================
# STEP 2: LOAD CONTRACT ADDRESSES
# =============================================================================

log_info "Loading contract addresses from deployments..."

require_file "$DEPLOYMENTS_FILE" "deployments.json (run entrypoint.sh first)"

# Parse contract addresses
WARM_STORAGE_CONTRACT_ADDRESS=$(jq -r '.FWSS_PROXY_ADDRESS // empty' "$DEPLOYMENTS_FILE")
WARM_STORAGE_VIEW_ADDRESS=$(jq -r '.FWSS_VIEW_ADDRESS // empty' "$DEPLOYMENTS_FILE")
SP_REGISTRY_ADDRESS=$(jq -r '.SERVICE_PROVIDER_REGISTRY_PROXY_ADDRESS // empty' "$DEPLOYMENTS_FILE")
PDP_VERIFIER_ADDRESS=$(jq -r '.PDP_VERIFIER_PROXY_ADDRESS // empty' "$DEPLOYMENTS_FILE")
PAYMENTS_ADDRESS=$(jq -r '.FILECOIN_PAY_ADDRESS // empty' "$DEPLOYMENTS_FILE")
USDFC_ADDRESS=$(jq -r '.USDFC_ADDRESS // empty' "$DEPLOYMENTS_FILE")
MULTICALL3_ADDRESS=$(jq -r '.MULTICALL3_ADDRESS // empty' "$DEPLOYMENTS_FILE")

log_info "Contract addresses loaded:"
log_info "  Warm Storage:  $WARM_STORAGE_CONTRACT_ADDRESS"
log_info "  SP Registry:   $SP_REGISTRY_ADDRESS"
log_info "  PDP Verifier:  $PDP_VERIFIER_ADDRESS"
log_info "  USDFC:         $USDFC_ADDRESS"
log_info "  Multicall3:    $MULTICALL3_ADDRESS"

# =============================================================================
# STEP 3: LOAD PRIVATE KEYS
# =============================================================================

log_info "Loading private keys..."

# Get deployer private key from accounts file
require_file "$ACCOUNTS_FILE" "accounts.json"
DEPLOYER_PRIVATE_KEY=$(jq -r '.accounts.deployer.privateKey' "$ACCOUNTS_FILE")
require_var "$DEPLOYER_PRIVATE_KEY" "deployer private key"

# Create client wallet
log_info "Creating client wallet..."
filwizard wallet create \
    --type ethereum \
    --count 1 \
    --fund 5 \
    --show-private-key \
    --key-output "$ACCOUNTS_FILE" \
    --name "client"

CLIENT_PRIVATE_KEY=$(jq -r '.accounts.client.privateKey' "$ACCOUNTS_FILE")
require_var "$CLIENT_PRIVATE_KEY" "client private key"
log_success "Client wallet created"

# Wait for SP private key from Curio
wait_for_file "$SP_PRIVATE_KEY_FILE" "SP private key from Curio"
SP_PRIVATE_KEY=$(cat "$SP_PRIVATE_KEY_FILE" | tr -d '[:space:]')
require_var "$SP_PRIVATE_KEY" "SP private key"
log_success "SP private key loaded"

# =============================================================================
# STEP 4: CREATE SYNAPSE ENV FILE
# =============================================================================

log_info "Creating synapse-sdk environment file..."

cat > "$ENV_FILE" << EOF
# Synapse SDK Environment Configuration
# Generated by setup-synapse.sh
# Network: $NETWORK

# Network Settings
NETWORK=$NETWORK
LOCALNET_CHAIN_ID=$CHAIN_ID
LOCALNET_RPC_URL=$RPC_URL
LOCALNET_RPC_WS_URL=$RPC_WS_URL

# Contract Addresses
LOCALNET_MULTICALL3_ADDRESS=$MULTICALL3_ADDRESS
LOCALNET_USDFC_ADDRESS=$USDFC_ADDRESS
LOCALNET_WARM_STORAGE_CONTRACT_ADDRESS=$WARM_STORAGE_CONTRACT_ADDRESS
LOCALNET_WARM_STORAGE_VIEW_ADDRESS=$WARM_STORAGE_VIEW_ADDRESS
LOCALNET_SP_REGISTRY_ADDRESS=$SP_REGISTRY_ADDRESS
LOCALNET_PDP_VERIFIER_ADDRESS=$PDP_VERIFIER_ADDRESS
LOCALNET_PAYMENTS_ADDRESS=$PAYMENTS_ADDRESS

# Private Keys
DEPLOYER_PRIVATE_KEY=$DEPLOYER_PRIVATE_KEY
SP_PRIVATE_KEY=$SP_PRIVATE_KEY
CLIENT_PRIVATE_KEY=$CLIENT_PRIVATE_KEY

# Service Provider
SP_SERVICE_URL=$SP_SERVICE_URL
EOF

log_success "Environment file created: $ENV_FILE"

# =============================================================================
# STEP 5: MINT TOKENS
# =============================================================================

log_info "Minting tokens for client and SP..."

# Mint USDFC for client (1000 USDFC)
log_info "  Minting 1000 USDFC for client..."
filwizard payments mint-private-key \
    --workspace "$WORKSPACE_PATH" \
    --private-key "$CLIENT_PRIVATE_KEY" \
    --amount 1000000000000000000000 \
    --fil 0

# Mint FIL for client (10 FIL)
log_info "  Minting 10 FIL for client..."
filwizard payments mint-private-key \
    --workspace "$WORKSPACE_PATH" \
    --private-key "$CLIENT_PRIVATE_KEY" \
    --amount 0 \
    --fil 10

# Mint USDFC for SP (10000 USDFC)
log_info "  Minting 10000 USDFC for SP..."
filwizard payments mint-private-key \
    --workspace "$WORKSPACE_PATH" \
    --private-key "$SP_PRIVATE_KEY" \
    --amount 10000000000000000000000 \
    --fil 0

# Mint FIL for SP (10 FIL)
log_info "  Minting 10 FIL for SP..."
filwizard payments mint-private-key \
    --workspace "$WORKSPACE_PATH" \
    --private-key "$SP_PRIVATE_KEY" \
    --amount 0 \
    --fil 10

log_success "Tokens minted"

# =============================================================================
# STEP 6: REGISTER SERVICE PROVIDER
# =============================================================================

log_info "Registering service provider..."

# Export environment variables for post-deploy-setup.js
export NETWORK
export RPC_URL
export DEPLOYER_PRIVATE_KEY
export SP_PRIVATE_KEY
export CLIENT_PRIVATE_KEY
export SP_SERVICE_URL
export SP_NAME
export SP_DESCRIPTION
export WARM_STORAGE_CONTRACT_ADDRESS
export SP_REGISTRY_ADDRESS
export USDFC_ADDRESS
export MULTICALL3_ADDRESS

cd /opt/antithesis/synapse-sdk

log_info "Running post-deploy-setup.js..."
log_info "  Mode: provider"
log_info "  Network: $NETWORK"
log_info "  SP Name: $SP_NAME"
log_info "  SP Service URL: $SP_SERVICE_URL"

node /opt/antithesis/synapse-sdk/utils/post-deploy-setup.js \
    --mode both \
    --network "$NETWORK" \
    --rpc-url "$RPC_URL" \
    --warm-storage "$WARM_STORAGE_CONTRACT_ADDRESS" \
    --sp-registry "$SP_REGISTRY_ADDRESS" \
    --usdfc "$USDFC_ADDRESS" \
    --multicall3 "$MULTICALL3_ADDRESS"

log_success "Service provider registered"
# =============================================================================
# COMPLETE
# =============================================================================

log_success "Synapse SDK setup complete!"
