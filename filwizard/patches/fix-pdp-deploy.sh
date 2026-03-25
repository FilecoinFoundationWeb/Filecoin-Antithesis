#!/bin/bash
# Patches warm-storage-deploy-all.sh for PDPVerifier v3.2.0 constructor
# The upstream script passes 1 arg but the constructor now needs 4:
#   (uint64 _initializerVersion, address _usdfcTokenAddress, uint256 _usdfcSybilFee, address _paymentsContractAddress)
# Fix: deploy FilecoinPayV1 before PDPVerifier, then pass all 4 constructor args.

DEPLOY_SCRIPT="$1"

if [ -z "$DEPLOY_SCRIPT" ] || [ ! -f "$DEPLOY_SCRIPT" ]; then
    echo "Usage: $0 <path-to-warm-storage-deploy-all.sh>"
    exit 1
fi

# 1. Replace the PDPVerifier constructor args line (1 arg → 4 args)
#    Before: $PDP_INIT_COUNTER
#    After:  $PDP_INIT_COUNTER ${USDFC_TOKEN_ADDRESS:-0x0000000000000000000000000000000000000000} ${USDFC_SYBIL_FEE:-60000000000000000} ${FILECOIN_PAY_ADDRESS:-0x0000000000000000000000000000000000000000}
sed -i 's|"PDPVerifier implementation" \\$|"PDPVerifier implementation" \\|' "$DEPLOY_SCRIPT"
sed -i 's|    \$PDP_INIT_COUNTER$|    $PDP_INIT_COUNTER ${USDFC_TOKEN_ADDRESS:-0x0000000000000000000000000000000000000000} ${USDFC_SYBIL_FEE:-60000000000000000} ${FILECOIN_PAY_ADDRESS:-0x0000000000000000000000000000000000000000}|' "$DEPLOY_SCRIPT"

# 2. Move FilecoinPayV1 deployment (Step 3) before PDPVerifier (Step 1)
#    Insert FilecoinPayV1 deploy block right after SessionKeyRegistry (Step 0)
#    and remove the original Step 3 block
sed -i '/^# Step 0: Deploy or use existing SessionKeyRegistry/,/^deploy_session_key_registry_if_needed/{
    /^deploy_session_key_registry_if_needed/a\
\
# Step 0.5: Deploy FilecoinPayV1 early (needed for PDPVerifier constructor)\
deploy_implementation_if_needed \\\
    "FILECOIN_PAY_ADDRESS" \\\
    "lib/fws-payments/src/FilecoinPayV1.sol:FilecoinPayV1" \\\
    "FilecoinPayV1"
}' "$DEPLOY_SCRIPT"

# Remove the original Step 3 FilecoinPayV1 block (now redundant — deploy_implementation_if_needed skips if already set)
# No need to remove — the function checks if FILECOIN_PAY_ADDRESS is already set and skips.

echo "Patched $DEPLOY_SCRIPT for PDPVerifier v3.2.0 constructor"
