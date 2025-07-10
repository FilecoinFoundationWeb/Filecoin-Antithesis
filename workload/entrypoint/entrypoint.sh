#!/bin/bash

RPC_LOTUS="http://10.20.20.24:1234/rpc/v1"
echo "Workload [entrypoint]: synchronizing system time..."
# Attempt to sync time with NTP server
if ntpdate -q pool.ntp.org &>/dev/null; then
    # If query works, try to sync
    ntpdate -u pool.ntp.org || {
        echo "Warning: Time sync failed. If running in a container, it may need the SYS_TIME capability."
        echo "Run the container with: --cap-add SYS_TIME"
    }
else
    echo "Warning: Unable to query NTP servers. Check network connectivity."
fi

current_time=$(date -u "+%Y-%m-%d %H:%M:%S UTC")
echo "Current system time: $current_time"

# Waiting for the chain head to pass a certain height
INIT_BLOCK_HEIGHT="${INIT_BLOCK_HEIGHT:-5}"
BLOCK_HEIGHT_REACHED=0

echo "Workload [entrypoint]: waiting for block height to reach ${INIT_BLOCK_HEIGHT}"

while [ $INIT_BLOCK_HEIGHT -gt $BLOCK_HEIGHT_REACHED ]
do
    BLOCK_HEIGHT_REACHED=$(curl -X POST $RPC_LOTUS -H 'Content-Type: application/json' --data '{"jsonrpc":"2.0","id":1,"method":"Filecoin.ChainHead","params":[]}' | jq '.result.Height')
    echo "Workload [entrypoint]: block height check: reached ${BLOCK_HEIGHT_REACHED}"
    if [ $INIT_BLOCK_HEIGHT -le $BLOCK_HEIGHT_REACHED ]; then
        break
    fi
    sleep 5
done

echo "Workload [entrypoint]: chainhead has reached block height ${INIT_BLOCK_HEIGHT}"
sleep 30
# Create keystore directory
KEYSTORE_DIR="/opt/antithesis/eth-keystore"
mkdir -p $KEYSTORE_DIR

# Create shared directory for contract addresses
SHARED_DIR="/root/devgen/contracts"
mkdir -p $SHARED_DIR

echo "Creating and funding Ethereum keystore..."
/opt/antithesis/app -operation createEthKeystore -keystore-dir $KEYSTORE_DIR

# Export required environment variables for deploy-devnet.sh
export KEYSTORE="$KEYSTORE_DIR/pdp-keystore.json"
export PASSWORD="password123"
export RPC_URL=$RPC_LOTUS

echo "Environment variables set:"
echo "KEYSTORE=$KEYSTORE"
echo "PASSWORD=$PASSWORD"
echo "RPC_URL=$RPC_LOTUS"

echo "Deploying PDP contracts..."
cd /opt/antithesis/pdp/tools

# Run deploy-devnet.sh once and capture all addresses
DEPLOY_OUTPUT=$(./deploy-devnet.sh 2>&1)
VERIFIER_IMPL=$(echo "$DEPLOY_OUTPUT" | awk '/verifier implementation deployed at:/ {print $NF}')
VERIFIER_PROXY=$(echo "$DEPLOY_OUTPUT" | awk '/verifier deployed at:/ && !/implementation/ {print $NF}')
SERVICE_IMPL=$(echo "$DEPLOY_OUTPUT" | awk '/service implementation deployed at:/ {print $NF}')
SERVICE_PROXY=$(echo "$DEPLOY_OUTPUT" | awk '/service deployed at:/ && !/implementation/ {print $NF}')

# Ensure the shared directory exists
mkdir -p "$SHARED_DIR"

# Save addresses to JSON file with proper formatting
cat > "$SHARED_DIR/contract-addresses.json" << EOL
{
    "verifier_implementation": "${VERIFIER_IMPL}",
    "verifier_proxy": "${VERIFIER_PROXY}",
    "service_implementation": "${SERVICE_IMPL}",
    "service_proxy": "${SERVICE_PROXY}",
    "deployment_time": "$(date -u +"%Y-%m-%dT%H:%M:%SZ")",
    "network": "devnet",
    "rpc_url": "${RPC_URL}",
    "keystore": "${KEYSTORE}",
    "deployment_status": "completed"
}
EOL

# Also save individual addresses for easier access
if [ ! -z "$VERIFIER_IMPL" ]; then
    echo "$VERIFIER_IMPL" > "$SHARED_DIR/verifier-implementation.addr"
fi
if [ ! -z "$VERIFIER_PROXY" ]; then
    echo "$VERIFIER_PROXY" > "$SHARED_DIR/verifier-proxy.addr"
fi
if [ ! -z "$SERVICE_IMPL" ]; then
    echo "$SERVICE_IMPL" > "$SHARED_DIR/service-implementation.addr"
fi
if [ ! -z "$SERVICE_PROXY" ]; then
    echo "$SERVICE_PROXY" > "$SHARED_DIR/service-proxy.addr"
fi

# Log the results
echo "Contract addresses have been saved to $SHARED_DIR"
echo "JSON file: $SHARED_DIR/contract-addresses.json"
echo "Individual address files have been created in $SHARED_DIR"
sleep 10
cd /opt/antithesis/payments/
ADDR=$(cast wallet address --keystore "$KEYSTORE" --password "$PASSWORD")
MAX_RETRIES=5
for i in $(seq 1 $MAX_RETRIES); do
    CURRENT_NONCE=$(cast nonce --rpc-url "$RPC_URL" $ADDR)
    if [ "$CURRENT_NONCE" -ge "4" ]; then
        break
    fi
    echo "Waiting for nonce to update (current: $CURRENT_NONCE, expected: >=4)"
    sleep 10
done
make deploy-devnet 2>&1 | tee /tmp/payments_deploy.log

# Extract Payments addresses from the deployment output
PAYMENTS_IMPL=$(grep "Implementation Address:" /tmp/payments_deploy.log | tail -n1 | awk '{print $3}')
PAYMENTS_PROXY=$(grep "Payments Contract Address:" /tmp/payments_deploy.log | tail -n1 | awk '{print $4}')

# Update the JSON file with Payments addresses
jq --arg impl "$PAYMENTS_IMPL" --arg proxy "$PAYMENTS_PROXY" '. + {
    "payments_implementation": $impl,
    "payments_proxy": $proxy
}' "$SHARED_DIR/contract-addresses.json" > /tmp/updated.json && mv /tmp/updated.json "$SHARED_DIR/contract-addresses.json"

# Save individual address files
if [ ! -z "$PAYMENTS_IMPL" ]; then
    echo "$PAYMENTS_IMPL" > "$SHARED_DIR/payments-implementation.addr"
fi
if [ ! -z "$PAYMENTS_PROXY" ]; then
    echo "$PAYMENTS_PROXY" > "$SHARED_DIR/payments-proxy.addr"
fi

# Verify the files were created
if [ -f "$SHARED_DIR/contract-addresses.json" ]; then
    echo "Contract addresses JSON file was created successfully"
else
    echo "Warning: Failed to create contract addresses JSON file"
fi

python3 -u /opt/antithesis/entrypoint/setup_complete.py

sleep infinity