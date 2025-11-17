#!/bin/bash

# Devnet Setup and E2E Test Script
# This script sets up the client, registers/approves SP, and runs e2e tests

set -e  # Exit on error

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
ENV_FILE="/opt/antithesis/synapse-sdk/.env.devnet"
WORKSPACE_PATH="/opt/antithesis/FilWizard/workspace"
DEPLOYMENTS_FILE="/opt/antithesis/FilWizard/workspace/deployments.json"

echo -e "${GREEN}=== Synapse SDK Setup ===${NC}\n"

# ============================================================================
# SETUP: Deploy Contracts
# ============================================================================
echo -e "${YELLOW}Checking if contracts are deployed...${NC}"
if [ ! -f "$DEPLOYMENTS_FILE" ]; then
    echo -e "${YELLOW}Contracts are not deployed. Deploying contracts...${NC}"
    if ! filwizard contract deploy-local --config /opt/antithesis/FilWizard/config/filecoin-synapse.json --workspace ./workspace --rpc-url "$FILECOIN_RPC" --create-deployer --bindings; then
        echo -e "${RED}ERROR: Contract deployment failed${NC}"
        exit 1
    fi
    echo -e "${GREEN}Contracts deployed successfully${NC}"
else
    echo -e "${GREEN}Contracts are already deployed${NC}"
fi

# ============================================================================
# SETUP: Extract Contract Addresses
# ============================================================================
echo -e "${YELLOW}Extracting contract addresses from deployments.json...${NC}"

if [ ! -f "$DEPLOYMENTS_FILE" ]; then
    echo -e "${RED}ERROR: deployments.json not found at $DEPLOYMENTS_FILE${NC}"
    exit 1
fi

# Debug: Show all contract names in deployments.json
echo -e "${YELLOW}Available contracts in deployments.json:${NC}"
cat "$DEPLOYMENTS_FILE" | jq -r '.[] | "  - \(.name): \(.address)"'

# Extract contract addresses (try multiple case variations and handle empty results)
WARM_STORAGE_CONTRACT_ADDRESS=$(cat "$DEPLOYMENTS_FILE" | jq -r '.[] | select(.name=="filecoinwarmstorageservice" or .name=="FilecoinWarmStorageService") | .address' | head -1)
WARM_STORAGE_VIEW_ADDRESS=$(cat "$DEPLOYMENTS_FILE" | jq -r '.[] | select(.name=="filecoinwarmstorageservicestateview" or .name=="FilecoinWarmStorageServiceStateView") | .address' | head -1)
SP_REGISTRY_ADDRESS=$(cat "$DEPLOYMENTS_FILE" | jq -r '.[] | select(.name=="serviceproviderregistry" or .name=="ServiceProviderRegistry") | .address' | head -1)
MULTICALL3_ADDRESS=$(cat "$DEPLOYMENTS_FILE" | jq -r '.[] | select(.name=="Multicall3" or .name=="multicall3" or .name=="MULTICALL3") | .address' | head -1)
USDFC_ADDRESS=$(cat "$DEPLOYMENTS_FILE" | jq -r '.[] | select(.name=="USDFC" or .name=="usdfc") | .address' | head -1)
PDP_VERIFIER_ADDRESS=$(cat "$DEPLOYMENTS_FILE" | jq -r '.[] | select(.name=="pdpverifier" or .name=="PDPVerifier") | .address' | head -1)

# Debug: Show extracted addresses
echo -e "${YELLOW}Extracted contract addresses:${NC}"
echo "  WARM_STORAGE_CONTRACT_ADDRESS: ${WARM_STORAGE_CONTRACT_ADDRESS:-<not found>}"
echo "  WARM_STORAGE_VIEW_ADDRESS: ${WARM_STORAGE_VIEW_ADDRESS:-<not found>}"
echo "  SP_REGISTRY_ADDRESS: ${SP_REGISTRY_ADDRESS:-<not found>}"
echo "  MULTICALL3_ADDRESS: ${MULTICALL3_ADDRESS:-<not found>}"
echo "  USDFC_ADDRESS: ${USDFC_ADDRESS:-<not found>}"
echo "  PDP_VERIFIER_ADDRESS: ${PDP_VERIFIER_ADDRESS:-<not found>}"

# Extract private keys
DEPLOYER_PRIVATE_KEY=$(cat "$DEPLOYMENTS_FILE" | jq -r '.[] | select(.name=="USDFC") | .deployer_private_key')

# Validate that we got all required addresses
if [ "$WARM_STORAGE_CONTRACT_ADDRESS" = "null" ] || [ -z "$WARM_STORAGE_CONTRACT_ADDRESS" ]; then
        echo -e "${RED}ERROR: Could not extract WARM_STORAGE_CONTRACT_ADDRESS${NC}"
        exit 1
fi

if [ "$WARM_STORAGE_VIEW_ADDRESS" = "null" ] || [ -z "$WARM_STORAGE_VIEW_ADDRESS" ]; then
    echo -e "${RED}ERROR: Could not extract WARM_STORAGE_VIEW_ADDRESS${NC}"
    exit 1
fi

if [ "$SP_REGISTRY_ADDRESS" = "null" ] || [ -z "$SP_REGISTRY_ADDRESS" ]; then
    echo -e "${RED}ERROR: Could not extract SP_REGISTRY_ADDRESS${NC}"
    exit 1
fi

# MULTICALL3 is optional - set to empty string if not found
if [ "$MULTICALL3_ADDRESS" = "null" ] || [ -z "$MULTICALL3_ADDRESS" ]; then
    echo -e "${YELLOW}WARNING: Could not extract MULTICALL3_ADDRESS, setting to empty string (optional)${NC}"
    MULTICALL3_ADDRESS=""
fi

if [ "$USDFC_ADDRESS" = "null" ] || [ -z "$USDFC_ADDRESS" ]; then
    echo -e "${RED}ERROR: Could not extract USDFC_ADDRESS${NC}"
    exit 1
fi

if [ "$PDP_VERIFIER_ADDRESS" = "null" ] || [ -z "$PDP_VERIFIER_ADDRESS" ]; then
    echo -e "${RED}ERROR: Could not extract PDP_VERIFIER_ADDRESS${NC}"
    exit 1
fi

if [ "$DEPLOYER_PRIVATE_KEY" = "null" ] || [ -z "$DEPLOYER_PRIVATE_KEY" ]; then
    echo -e "${RED}ERROR: Could not extract DEPLOYER_PRIVATE_KEY${NC}"
    exit 1
fi

echo -e "${GREEN}Contract addresses extracted successfully${NC}"

# ============================================================================
# SETUP: Create Client Private Key
# ============================================================================
echo -e "${YELLOW}Creating client private key using filwizard wallet create...${NC}"
CLIENT_KEYS_FILE="/tmp/client-keys.txt"
filwizard wallet create \
  --type ethereum \
  --count 1 \
  --fund 5 \
  --show-private-key \
  --key-output "$CLIENT_KEYS_FILE"

if [ ! -f "$CLIENT_KEYS_FILE" ]; then
    echo -e "${RED}ERROR: Failed to create client keys file${NC}"
    exit 1
fi

CLIENT_PRIVATE_KEY=$(grep "Private Key:" "$CLIENT_KEYS_FILE" | awk '{print $3}')
if [ -z "$CLIENT_PRIVATE_KEY" ]; then
    echo -e "${RED}ERROR: Failed to extract client private key from $CLIENT_KEYS_FILE${NC}"
    cat "$CLIENT_KEYS_FILE"
    exit 1
fi

echo -e "${GREEN}Client private key generated successfully${NC}"

# ============================================================================
# SETUP: Get SP Private Key from CURIO_DATA_DIR
# ============================================================================
echo -e "${YELLOW}Getting SP private key from CURIO_DATA_DIR...${NC}"
# Hardcode to /root/devgen/curio which is the shared volume mount point in workload container
# This maps to /var/lib/curio in the curio container
CURIO_DATA_DIR="/root/devgen/curio"
SP_PRIVATE_KEY_FILE="${CURIO_DATA_DIR}/private_key"

# Check if file exists at the expected path, if not try alternative paths
if [ ! -f "$SP_PRIVATE_KEY_FILE" ]; then
    echo -e "${YELLOW}File not found at $SP_PRIVATE_KEY_FILE, trying alternative paths...${NC}"
    for alt_path in "/root/devgen/curio/private_key" "/curio/private_key" "$(dirname "$SP_PRIVATE_KEY_FILE")/private_key"; do
        if [ -f "$alt_path" ]; then
            echo -e "${GREEN}Found private key at: $alt_path${NC}"
            SP_PRIVATE_KEY_FILE="$alt_path"
            break
        fi
    done
fi

echo -e "${YELLOW}Waiting for SP private key file to exist at $SP_PRIVATE_KEY_FILE...${NC}"
while [ ! -f "$SP_PRIVATE_KEY_FILE" ]; do
    echo "Waiting for SP private key file at $SP_PRIVATE_KEY_FILE..."
    echo "Checking if file exists at alternative locations..."
    ls -la /root/devgen/curio/private_key 2>/dev/null || echo "  /root/devgen/curio/private_key: not found"
    ls -la /curio/private_key 2>/dev/null || echo "  /curio/private_key: not found"
    sleep 2
done

echo -e "${GREEN}SP private key file found, reading contents...${NC}"
SP_PRIVATE_KEY=$(cat "$SP_PRIVATE_KEY_FILE")
if [ -z "$SP_PRIVATE_KEY" ]; then
    echo -e "${RED}ERROR: Failed to read SP private key from $SP_PRIVATE_KEY_FILE${NC}"
    exit 1
fi

# Remove any whitespace/newlines and add 0x prefix if it doesn't have one
SP_PRIVATE_KEY=$(echo "$SP_PRIVATE_KEY" | tr -d '[:space:]')
if [[ ! "$SP_PRIVATE_KEY" =~ ^0x ]]; then
    SP_PRIVATE_KEY="0x$SP_PRIVATE_KEY"
fi

echo -e "${GREEN}SP private key retrieved successfully${NC}"

# ============================================================================
# SETUP: Create .env.devnet File
# ============================================================================
echo -e "${YELLOW}Creating .env.devnet file for synapse SDK...${NC}"

cat > "$ENV_FILE" << EOF
NETWORK=devnet
RPC_URL=http://lotus-1:1234/rpc/v1
WARM_STORAGE_CONTRACT_ADDRESS=$WARM_STORAGE_CONTRACT_ADDRESS
WARM_STORAGE_VIEW_ADDRESS=$WARM_STORAGE_VIEW_ADDRESS
SP_REGISTRY_ADDRESS=$SP_REGISTRY_ADDRESS
MULTICALL3_ADDRESS=$MULTICALL3_ADDRESS
PDP_VERIFIER_ADDRESS=$PDP_VERIFIER_ADDRESS
DEPLOYER_PRIVATE_KEY=$DEPLOYER_PRIVATE_KEY
SP_PRIVATE_KEY=$SP_PRIVATE_KEY
SP_SERVICE_URL=http://curio:80
CLIENT_PRIVATE_KEY=$CLIENT_PRIVATE_KEY
USDFC_ADDRESS=$USDFC_ADDRESS
EOF

echo -e "${GREEN}Successfully created .env.devnet file at $ENV_FILE${NC}"

# Copy .env.devnet to shared volume for curio container access
# Hardcode to /root/devgen/curio which is the shared volume mount point in workload container
# This maps to /var/lib/curio in the curio container
CURIO_SHARED_DIR="/root/devgen/curio"
echo -e "${YELLOW}Copying .env.devnet to shared volume for curio...${NC}"
if [ ! -d "$CURIO_SHARED_DIR" ]; then
    echo -e "${YELLOW}Creating directory $CURIO_SHARED_DIR...${NC}"
    mkdir -p "$CURIO_SHARED_DIR"
fi
cp "$ENV_FILE" "$CURIO_SHARED_DIR/.env.devnet"
if [ -f "$CURIO_SHARED_DIR/.env.devnet" ]; then
    echo -e "${GREEN}Successfully copied .env.devnet to $CURIO_SHARED_DIR/.env.devnet${NC}"
    echo -e "${GREEN}File verification: $(ls -lh "$CURIO_SHARED_DIR/.env.devnet")${NC}"
else
    echo -e "${RED}ERROR: Failed to copy .env.devnet to $CURIO_SHARED_DIR/.env.devnet${NC}"
    exit 1
fi

# ============================================================================
# SETUP: Fund Client and SP Accounts
# ============================================================================
echo -e "${YELLOW}Funding client and SP accounts with USDFC tokens and FIL...${NC}"

echo -e "${YELLOW}Funding client account with USDFC tokens...${NC}"
if ! filwizard payments mint-private-key \
  --workspace "$WORKSPACE_PATH" \
  --private-key "$CLIENT_PRIVATE_KEY" \
  --amount 10000000000000000000 \
  --fil 0; then
    echo -e "${RED}ERROR: Failed to fund client account with USDFC${NC}"
    exit 1
fi

echo -e "${YELLOW}Funding client account with 10 FIL...${NC}"
if ! filwizard payments mint-private-key \
  --workspace "$WORKSPACE_PATH" \
  --private-key "$CLIENT_PRIVATE_KEY" \
  --amount 0 \
  --fil 10; then
    echo -e "${RED}ERROR: Failed to fund client account with FIL${NC}"
    exit 1
fi

echo -e "${YELLOW}Funding SP account with USDFC tokens...${NC}"
if ! filwizard payments mint-private-key \
  --workspace "$WORKSPACE_PATH" \
  --private-key "$SP_PRIVATE_KEY" \
  --amount 10000000000000000000 \
  --fil 0; then
    echo -e "${RED}ERROR: Failed to fund SP account with USDFC${NC}"
    exit 1
fi

echo -e "${YELLOW}Funding SP account with 10 FIL...${NC}"
if ! filwizard payments mint-private-key \
  --workspace "$WORKSPACE_PATH" \
  --private-key "$SP_PRIVATE_KEY" \
  --amount 0 \
  --fil 10; then
    echo -e "${RED}ERROR: Failed to fund SP account with FIL${NC}"
    exit 1
fi

echo -e "${GREEN}Successfully funded both client and SP accounts${NC}"

# Clean up temporary files
rm -f "$CLIENT_KEYS_FILE"

echo -e "${GREEN}=== Synapse SDK Setup Completed Successfully ===${NC}\n"

# ============================================================================
# E2E TEST SECTION
# ============================================================================
echo -e "${GREEN}=== Devnet Setup and E2E Test ===${NC}\n"

# Load environment variables
export $(cat "$ENV_FILE" | grep -v '^#' | xargs)

# Change to synapse-sdk root
cd /opt/antithesis/synapse-sdk
export ENV_FILE="/opt/antithesis/synapse-sdk/.env.devnet"
# ============================================================================
# STEP 1: Test Curio Connectivity
# ============================================================================
echo -e "${YELLOW}Step 1: Testing Curio Connectivity...${NC}"
export SERVICE_URL="${SP_SERVICE_URL:-http://curio:80}"
node /opt/antithesis/synapse-sdk/utils/debug-curio-ping.js
if [ $? -ne 0 ]; then
    echo -e "${RED}Curio connectivity test failed!${NC}"
    exit 1
fi
echo ""

# ============================================================================
# STEP 2: Register Service Provider
# ============================================================================
echo -e "${YELLOW}Step 2: Registering Service Provider...${NC}"
node --env-file="$ENV_FILE" /opt/antithesis/synapse-sdk/utils/sp-tool.js register \
    --name "${SP_NAME:-My Devnet Provider}" \
    --http "${SP_SERVICE_URL:-http://curio:80}" \
    --network devnet

if [ $? -ne 0 ]; then
    echo -e "${RED}SP registration failed!${NC}"
    exit 1
fi

# Get the provider ID (you may need to adjust this based on output)
echo -e "${GREEN}SP registered successfully!${NC}"
echo -e "${YELLOW}Note: Please note the Provider ID from the output above${NC}\n"

# ============================================================================
# STEP 3: Get Provider Info (to verify registration)
# ============================================================================
echo -e "${YELLOW}Step 3: Getting Provider Info...${NC}"
read -p "Enter Provider ID (from Step 2): " PROVIDER_ID
node --env-file="$ENV_FILE" /opt/antithesis/synapse-sdk/utils/sp-tool.js info --id "$PROVIDER_ID" --network devnet
echo ""

# ============================================================================
# STEP 4: Add SP to WarmStorage Approved List
# ============================================================================
echo -e "${YELLOW}Step 4: Adding SP to WarmStorage Approved List...${NC}"
node --env-file="$ENV_FILE" /opt/antithesis/synapse-sdk/utils/sp-tool.js warm-add \
    --id "$PROVIDER_ID" \
    --network devnet

if [ $? -ne 0 ]; then
    echo -e "${RED}Failed to add SP to WarmStorage!${NC}"
    exit 1
fi
echo ""

# ============================================================================
# STEP 5: Setup Client Payment Approvals
# ============================================================================
echo -e "${YELLOW}Step 5: Setting up Client Payment Approvals...${NC}"
node --env-file="$ENV_FILE" /opt/antithesis/synapse-sdk/utils/post-deploy-setup.js --mode client

if [ $? -ne 0 ]; then
    echo -e "${RED}Client setup failed!${NC}"
    exit 1
fi
echo ""

# ============================================================================
# STEP 6: Test Piece Upload Flow (Optional - for debugging)
# ============================================================================
echo -e "${YELLOW}Step 6: Testing Piece Upload Flow (optional)...${NC}"
read -p "Test piece upload flow? (y/n): " TEST_UPLOAD
if [ "$TEST_UPLOAD" = "y" ]; then
    export SERVICE_URL="${SP_SERVICE_URL:-http://curio:80}"
    node /opt/antithesis/synapse-sdk/utils/test-piece-upload-flow.js
    echo ""
fi

# ============================================================================
# STEP 7: Run E2E Test
# ============================================================================
echo -e "${YELLOW}Step 7: Running E2E Storage Test...${NC}"
read -p "Enter test file path (default: test.txt): " TEST_FILE
TEST_FILE="${TEST_FILE:-test.txt}"

if [ ! -f "$TEST_FILE" ]; then
    echo -e "${YELLOW}Creating test file: $TEST_FILE${NC}"
    echo "Hello, Filecoin Warm Storage!" > "$TEST_FILE"
fi

node --env-file="$ENV_FILE" /opt/antithesis/synapse-sdk/utils/example-storage-e2e.js "$TEST_FILE"

if [ $? -eq 0 ]; then
    echo -e "\n${GREEN}=== E2E Test Completed Successfully! ===${NC}"
else
    echo -e "\n${RED}=== E2E Test Failed ===${NC}"
    exit 1
fi

