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

echo FILECOIN_RPC="$FILECOIN_RPC"

echo ETH_RPC_URL="$ETH_RPC_URL"

if [ ! -f "$DEPLOYMENTS_FILE" ]; then
    filwizard contract deploy-local --config /opt/antithesis/FilWizard/config/filecoin-synapse.json --workspace ./workspace --rpc-url "$FILECOIN_RPC" --create-deployer --bindings || exit 1
fi

if [ ! -f "$DEPLOYMENTS_FILE" ]; then
    echo -e "${RED}ERROR: deployments.json not found${NC}" >&2
    exit 1
fi

WARM_STORAGE_CONTRACT_ADDRESS=$(cat "$DEPLOYMENTS_FILE" | jq -r '.[] | select(.name=="filecoinwarmstorageservice" or .name=="FilecoinWarmStorageService") | .address' | head -1)
WARM_STORAGE_VIEW_ADDRESS=$(cat "$DEPLOYMENTS_FILE" | jq -r '.[] | select(.name=="filecoinwarmstorageservicestateview" or .name=="FilecoinWarmStorageServiceStateView") | .address' | head -1)
SP_REGISTRY_ADDRESS=$(cat "$DEPLOYMENTS_FILE" | jq -r '.[] | select(.name=="serviceproviderregistry" or .name=="ServiceProviderRegistry") | .address' | head -1)
MULTICALL3_ADDRESS=$(cat "$DEPLOYMENTS_FILE" | jq -r '.[] | select(.name=="Multicall3" or .name=="multicall3" or .name=="MULTICALL3") | .address' | head -1)
USDFC_ADDRESS=$(cat "$DEPLOYMENTS_FILE" | jq -r '.[] | select(.name=="USDFC" or .name=="usdfc") | .address' | head -1)
PDP_VERIFIER_ADDRESS=$(cat "$DEPLOYMENTS_FILE" | jq -r '.[] | select(.name=="pdpverifier" or .name=="PDPVerifier") | .address' | head -1)
DEPLOYER_PRIVATE_KEY=$(cat "$DEPLOYMENTS_FILE" | jq -r '.[] | select(.name=="USDFC") | .deployer_private_key')

[ -z "$WARM_STORAGE_CONTRACT_ADDRESS" ] || [ "$WARM_STORAGE_CONTRACT_ADDRESS" = "null" ] && { echo -e "${RED}ERROR: WARM_STORAGE_CONTRACT_ADDRESS not found${NC}" >&2; exit 1; }
[ -z "$WARM_STORAGE_VIEW_ADDRESS" ] || [ "$WARM_STORAGE_VIEW_ADDRESS" = "null" ] && { echo -e "${RED}ERROR: WARM_STORAGE_VIEW_ADDRESS not found${NC}" >&2; exit 1; }
[ -z "$SP_REGISTRY_ADDRESS" ] || [ "$SP_REGISTRY_ADDRESS" = "null" ] && { echo -e "${RED}ERROR: SP_REGISTRY_ADDRESS not found${NC}" >&2; exit 1; }
[ -z "$USDFC_ADDRESS" ] || [ "$USDFC_ADDRESS" = "null" ] && { echo -e "${RED}ERROR: USDFC_ADDRESS not found${NC}" >&2; exit 1; }
[ -z "$PDP_VERIFIER_ADDRESS" ] || [ "$PDP_VERIFIER_ADDRESS" = "null" ] && { echo -e "${RED}ERROR: PDP_VERIFIER_ADDRESS not found${NC}" >&2; exit 1; }
[ -z "$DEPLOYER_PRIVATE_KEY" ] || [ "$DEPLOYER_PRIVATE_KEY" = "null" ] && { echo -e "${RED}ERROR: DEPLOYER_PRIVATE_KEY not found${NC}" >&2; exit 1; }

[ -z "$MULTICALL3_ADDRESS" ] || [ "$MULTICALL3_ADDRESS" = "null" ] && MULTICALL3_ADDRESS=""

CLIENT_KEYS_FILE="/tmp/client-keys.txt"
filwizard wallet create --type ethereum --count 1 --fund 5 --show-private-key --key-output "$CLIENT_KEYS_FILE" || exit 1
CLIENT_PRIVATE_KEY=$(grep "Private Key:" "$CLIENT_KEYS_FILE" | awk '{print $3}')
[ -z "$CLIENT_PRIVATE_KEY" ] && { echo -e "${RED}ERROR: Failed to extract client private key${NC}" >&2; exit 1; }

CURIO_DATA_DIR="/root/devgen/curio"
SP_PRIVATE_KEY_FILE="${CURIO_DATA_DIR}/private_key"

[ ! -f "$SP_PRIVATE_KEY_FILE" ] && for alt_path in "/root/devgen/curio/private_key" "/curio/private_key"; do
    [ -f "$alt_path" ] && SP_PRIVATE_KEY_FILE="$alt_path" && break
done

while [ ! -f "$SP_PRIVATE_KEY_FILE" ]; do
    sleep 2
done

SP_PRIVATE_KEY=$(cat "$SP_PRIVATE_KEY_FILE" | tr -d '[:space:]')
[ -z "$SP_PRIVATE_KEY" ] && { echo -e "${RED}ERROR: Failed to read SP private key${NC}" >&2; exit 1; }
[[ ! "$SP_PRIVATE_KEY" =~ ^0x ]] && SP_PRIVATE_KEY="0x$SP_PRIVATE_KEY"

cat > "$ENV_FILE" << EOF
NETWORK=devnet
RPC_URL=http://lotus0:1234/rpc/v1
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

CURIO_SHARED_DIR="/root/devgen/curio"
mkdir -p "$CURIO_SHARED_DIR"
cp "$ENV_FILE" "$CURIO_SHARED_DIR/.env.devnet" || exit 1

filwizard payments mint-private-key --workspace "$WORKSPACE_PATH" --private-key "$CLIENT_PRIVATE_KEY" --amount 1000000000000000000000 --fil 0 || exit 1
filwizard payments mint-private-key --workspace "$WORKSPACE_PATH" --private-key "$CLIENT_PRIVATE_KEY" --amount 0 --fil 10 || exit 1
filwizard payments mint-private-key --workspace "$WORKSPACE_PATH" --private-key "$SP_PRIVATE_KEY" --amount 10000000000000000000000 --fil 0 || exit 1
filwizard payments mint-private-key --workspace "$WORKSPACE_PATH" --private-key "$SP_PRIVATE_KEY" --amount 0 --fil 10 || exit 1
rm -f "$CLIENT_KEYS_FILE"

export $(cat "$ENV_FILE" | grep -v '^#' | xargs)
cd /opt/antithesis/synapse-sdk
export ENV_FILE="/opt/antithesis/synapse-sdk/.env.devnet"
export SERVICE_URL="${SP_SERVICE_URL:-http://curio:80}"

REGISTER_OUTPUT=$(node --env-file="$ENV_FILE" /opt/antithesis/synapse-sdk/utils/sp-tool.js register \
    --name "${SP_NAME:-My Devnet Provider}" \
    --http "${SP_SERVICE_URL:-http://curio:80}" \
    --network devnet 2>&1) || { echo "$REGISTER_OUTPUT" >&2; exit 1; }
echo $REGISTER_OUTPUT
PROVIDER_ID=$(echo "$REGISTER_OUTPUT" | sed -n 's/.*Provider registered with ID: \([0-9]*\).*/\1/p' | head -1)
[ -z "$PROVIDER_ID" ] && { echo -e "${RED}ERROR: Could not extract Provider ID${NC}" >&2; exit 1; }
echo $PROVIDER_ID
node --env-file="$ENV_FILE" /opt/antithesis/synapse-sdk/utils/sp-tool.js info --id "$PROVIDER_ID" --network devnet || exit 1
node --env-file="$ENV_FILE" /opt/antithesis/synapse-sdk/utils/sp-tool.js warm-add --id "$PROVIDER_ID" --network devnet || exit 1
node --env-file="$ENV_FILE" /opt/antithesis/synapse-sdk/utils/post-deploy-setup.js --mode client 
ls
node --env-file="$ENV_FILE" /opt/antithesis/synapse-sdk/utils/example-storage-e2e.js /opt/antithesis/synapse-sdk/README.md

sleep infinity
