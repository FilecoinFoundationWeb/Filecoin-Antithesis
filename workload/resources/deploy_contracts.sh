#!/bin/bash

# Deploy all smart contracts using cast
# This script loops through all smart contract files and deploys them

set -e

# Configuration
SHARED_DIR="/opt/antithesis/shared"
CONTRACTS_DIR="/opt/antithesis/resources/smart-contracts"
RPC_URL="http://10.20.20.26:1235/rpc/v0"  # From entrypoint.sh
WALLET_INFO_FILE="$SHARED_DIR/wallet_info.json"
CONTRACT_ADDRESSES_FILE="$SHARED_DIR/contract_addresses.json"
DEPLOYMENT_INFO_FILE="$SHARED_DIR/deployment_info.json"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Function to print colored output
print_status() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check if required tools are available
check_dependencies() {
    print_status "Checking dependencies..."
    
    if ! command -v cast &> /dev/null; then
        print_error "cast is not installed. Please install foundry first."
        exit 1
    fi
    
    if ! command -v jq &> /dev/null; then
        print_error "jq is not installed. Please install jq first."
        exit 1
    fi
    
    print_success "All dependencies are available"
}

# Check if wallet info exists
check_wallet() {
    print_status "Checking wallet information..."
    
    if [ ! -f "$WALLET_INFO_FILE" ]; then
        print_error "Wallet info file not found at $WALLET_INFO_FILE"
        print_error "Please create a wallet first using: ./workload wallet create-eth-keystore --node Lotus2"
        exit 1
    fi
    
    # Extract wallet information
    WALLET_ADDRESS=$(jq -r '.address' "$WALLET_INFO_FILE")
    KEYSTORE_PATH=$(jq -r '.keystore_path' "$WALLET_INFO_FILE")
    PASSWORD=$(jq -r '.password' "$WALLET_INFO_FILE")
    
    if [ "$WALLET_ADDRESS" = "null" ] || [ -z "$WALLET_ADDRESS" ]; then
        print_error "Invalid wallet address in $WALLET_INFO_FILE"
        exit 1
    fi
    
    print_success "Using wallet: $WALLET_ADDRESS"
    print_success "Keystore path: $KEYSTORE_PATH"
}

# Initialize contract addresses file
init_contract_addresses() {
    print_status "Initializing contract addresses file..."
    
    if [ ! -f "$CONTRACT_ADDRESSES_FILE" ]; then
        cat > "$CONTRACT_ADDRESSES_FILE" << EOF
{
  "contracts": {},
  "deployment_timestamp": "$(date -u +"%Y-%m-%dT%H:%M:%SZ")",
  "network": "filecoin-calibration"
}
EOF
        print_success "Created contract addresses file"
    else
        print_status "Contract addresses file already exists"
    fi
}

# Deploy a single contract
deploy_contract() {
    local contract_name=$1
    local hex_file=$2
    local sol_file=$3
    
    print_status "Deploying $contract_name..."
    
    # Check if contract is already deployed
    if jq -e ".contracts.$contract_name" "$CONTRACT_ADDRESSES_FILE" > /dev/null 2>&1; then
        local existing_address=$(jq -r ".contracts.$contract_name.address" "$CONTRACT_ADDRESSES_FILE")
        print_warning "$contract_name already deployed at $existing_address, skipping..."
        return 0
    fi
    
    # Deploy using cast
    print_status "Deploying $contract_name from $hex_file..."
    
    # Use cast to deploy the contract
    local deploy_output
    if deploy_output=$(cast send --from "$WALLET_ADDRESS" --keystore "$KEYSTORE_PATH" --password "$PASSWORD" --rpc-url "$RPC_URL" --create "$(cat "$hex_file")" 2>&1); then
        # Extract contract address from deployment output
        local contract_address=$(echo "$deploy_output" | grep -o "contractAddress: 0x[a-fA-F0-9]*" | cut -d' ' -f2)
        
        if [ -n "$contract_address" ]; then
            # Update contract addresses file
            jq --arg name "$contract_name" \
               --arg address "$contract_address" \
               --arg hex_file "$hex_file" \
               --arg sol_file "$sol_file" \
               --arg timestamp "$(date -u +"%Y-%m-%dT%H:%M:%SZ")" \
               '.contracts[$name] = {
                 "address": $address,
                 "hex_file": $hex_file,
                 "sol_file": $sol_file,
                 "deployed_at": $timestamp
               }' "$CONTRACT_ADDRESSES_FILE" > "$CONTRACT_ADDRESSES_FILE.tmp" && \
               mv "$CONTRACT_ADDRESSES_FILE.tmp" "$CONTRACT_ADDRESSES_FILE"
            
            print_success "$contract_name deployed successfully at $contract_address"
        else
            print_error "Failed to extract contract address from deployment output"
            print_error "Deployment output: $deploy_output"
            return 1
        fi
    else
        print_error "Failed to deploy $contract_name"
        print_error "Deployment output: $deploy_output"
        return 1
    fi
}

# Main deployment function
deploy_all_contracts() {
    print_status "Starting deployment of all smart contracts..."
    
    # List of contracts to deploy
    local contracts=(
        "SimpleCoin:SimpleCoin.hex:SimpleCoin.sol"
        "MCopy:MCopy.hex:MCopy.sol"
        "TransientStorage:TransientStorage.hex:TransientStorage.sol"
    )
    
    local success_count=0
    local total_count=${#contracts[@]}
    
    for contract_info in "${contracts[@]}"; do
        IFS=':' read -r contract_name hex_file sol_file <<< "$contract_info"
        
        if deploy_contract "$contract_name" "$CONTRACTS_DIR/$hex_file" "$CONTRACTS_DIR/$sol_file"; then
            ((success_count++))
        fi
        
        # Add a small delay between deployments
        sleep 2
    done
    
    print_success "Deployment completed: $success_count/$total_count contracts deployed successfully"
}

# Update deployment info
update_deployment_info() {
    print_status "Updating deployment information..."
    
    cat > "$DEPLOYMENT_INFO_FILE" << EOF
{
  "last_deployment": "$(date -u +"%Y-%m-%dT%H:%M:%SZ")",
  "total_contracts": $(jq '.contracts | length' "$CONTRACT_ADDRESSES_FILE"),
  "deployed_contracts": $(jq -r '.contracts | keys | join(", ")' "$CONTRACT_ADDRESSES_FILE"),
  "wallet_used": "$WALLET_ADDRESS",
  "rpc_url": "$RPC_URL"
}
EOF
    
    print_success "Deployment info updated"
}

# Main execution
main() {
    print_status "Starting smart contract deployment process..."
    
    # Check dependencies
    check_dependencies
    
    # Check wallet
    check_wallet
    
    # Initialize files
    init_contract_addresses
    
    # Deploy contracts
    deploy_all_contracts
    
    # Update deployment info
    update_deployment_info
    
    print_success "Smart contract deployment process completed!"
    print_status "Contract addresses saved to: $CONTRACT_ADDRESSES_FILE"
    print_status "Deployment info saved to: $DEPLOYMENT_INFO_FILE"
}

# Run main function
main "$@"
