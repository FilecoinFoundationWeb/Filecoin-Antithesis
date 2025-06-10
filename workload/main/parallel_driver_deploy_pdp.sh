#!/bin/bash

# Set RPC URL
export RPC_URL="http://lotus-1:1234/rpc/v1"

pwd
# Set keystore path and password as used in geth_keystore.go
export KEYSTORE="/opt/antithesis/resources/smart-contracts/keystore"
export PASSWORD="testpassword123"

# Find the keystore file (it starts with "UTC--")
KEYSTORE_FILE=$(find $KEYSTORE -type f -name "UTC--*" | head -n 1)
if [ -z "$KEYSTORE_FILE" ]; then
    echo "Error: No keystore file found in $KEYSTORE"
    exit 1
fi

# Get the address from the keystore file
clientAddr=$(cat $KEYSTORE_FILE | jq -r '.address')
if [ -z "$clientAddr" ]; then
    echo "Error: Could not extract address from keystore file"
    exit 1
fi

# Add 0x prefix if not present
if [[ ! "$clientAddr" =~ ^0x ]]; then
    clientAddr="0x$clientAddr"
fi

NONCE="$(cast nonce --rpc-url "$RPC_URL" "$clientAddr")"

echo "clientAddr: $clientAddr"
echo "NONCE: $NONCE"

# Check ETH balance using cast
echo "Checking balance..."
BALANCE=$(cast balance --rpc-url "$RPC_URL" "$clientAddr")
echo "ETH Balance: $BALANCE"

# Change to the pdp directory where node_modules and dependencies are installed
cd /opt/antithesis/pdp

echo "Deploying PDP verifier"

# Parse the output of forge create to extract the contract address
VERIFIER_IMPLEMENTATION_ADDRESS=$(forge create --rpc-url "$RPC_URL" --keystore "$KEYSTORE_FILE" --password "$PASSWORD" --nonce $NONCE --broadcast src/PDPVerifier.sol:PDPVerifier | grep "Deployed to" | awk '{print $3}')
if [ -z "$VERIFIER_IMPLEMENTATION_ADDRESS" ]; then
    echo "Error: Failed to extract PDP verifier contract address"
    exit 1
fi
echo "PDP verifier implementation deployed at: $VERIFIER_IMPLEMENTATION_ADDRESS"

# Deploy PDP verifier
forge create --rpc-url "$RPC_URL" --keystore "$KEYSTORE_FILE" --password "$PASSWORD" --nonce $NONCE --broadcast src/PDPVerifier.sol:PDPVerifier | grep "Deployed to" | awk '{print $3}'