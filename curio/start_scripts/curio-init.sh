#!/usr/bin/env bash
set -e
echo CURIO_REPO_PATH=$CURIO_REPO_PATH
export LOTUS_PATH=${LOTUS_0_PATH}
echo LOTUS_PATH=$LOTUS_PATH
echo Wait for lotus is ready ...
lotus wait-api
head=0
# Loop until the head is greater than 9
while [[ $head -le 5 ]]; do
    head=$(lotus chain list | awk '{print $1}' | awk -F':' '{print $1}' | tail -1)
    if [[ $head -le 5 ]]; then
        echo "Current head: $head, which is not greater than 5. Waiting..."
        sleep 1  # Wait for 4 seconds before checking again
    else
        echo "The head is now greater than 5: $head"
    fi
done

echo All ready. Lets go
# myip=`nslookup curio | grep -v "#" | grep Address | awk '{print $2}'`
myip=$(getent hosts curio | awk '{print $1}')

if [ ! -f $CURIO_REPO_PATH/.init.curio ]; then
  echo Wait for lotus-miner is ready ...
  lotus wait-api

  if [ ! -f $CURIO_REPO_PATH/.init.setup ]; then
    export DEFAULT_WALLET=`lotus wallet default`
    lotus-shed miner create --deposit-margin-factor 1.01 $DEFAULT_WALLET $DEFAULT_WALLET $DEFAULT_WALLET 8MiB
    touch $CURIO_REPO_PATH/.init.setup
  fi

  if [ ! -f $CURIO_REPO_PATH/.init.config ]; then
    newminer=`lotus state list-miners | grep -E -v 't01000|t01001'`
    echo "New Miner is $newminer"
    echo Initiating a new Curio cluster ...
    curio config new-cluster $newminer
    echo Creating market config...
    curio config get base | sed -e 's/#Miners = \[\]/Miners = ["'"$newminer"'"]/g' | curio config set --title base
    
    # Set up base layer configuration
    CONFIG_CONTENT='[HTTP]
      Enable = true
      DelegateTLS = true
      DomainName = "curio"
      ListenAddress = "0.0.0.0:80"

    [Subsystems]
      EnableCommP = true
      EnableParkPiece = true
      EnablePDP = true
      EnableMoveStorage = true
      EnableDealMarket = true
      EnableWebGui = true
      GuiAddress = "0.0.0.0:4701"
    '
    echo "$CONFIG_CONTENT" | curio config create --title pdp-only
    touch $CURIO_REPO_PATH/.init.config
  fi

  # Add storage attachment
  echo "Starting Curio node to attach storage..."
  CURIO_FAKE_CPU=5 curio run --nosync --layers seal,post,pdp-only,gui &
  CURIO_PID=$!
  sleep 20
  
  until curio cli --machine $myip:12300 wait-api; do
    echo "Waiting for the curio CLI to become ready..."
    sleep 5
  done
  
  curio cli --machine $myip:12300 storage attach --init --seal --store $CURIO_REPO_PATH
  
  echo "Stopping temporary Curio node..."
  kill -15 $CURIO_PID || kill -9 $CURIO_PID

  touch $CURIO_REPO_PATH/.init.curio
fi

# Setup PDP service if not already done
if [ ! -f $CURIO_REPO_PATH/.init.pdp ]; then
  echo "Setting up PDP service..."
  
  # Start Curio node first
  echo "Starting Curio node for PDP setup..."
  CURIO_FAKE_CPU=5 curio run --nosync --layers seal,post,pdp-only,gui &
  CURIO_PID=$!
    sleep 20
  # Wait for the node to be ready using curio cli
  echo "Waiting for Curio API to be ready..."
  until curio cli --machine $myip:12300 wait-api; do
    echo "Waiting for the curio CLI to become ready..."
    sleep 5
  done
  
  # Create service secret and save public key
  echo "Creating PDP service secret..."
  cd $CURIO_REPO_PATH
  pdptool create-service-secret > pdp_service_key.txt

  # Extract public key from the output and properly format it
  PUB_KEY=$(cat pdp_service_key.txt | sed -n '/Public Key:/,/-----END PUBLIC KEY-----/p' | grep -v "Public Key:" | sed 's/^[[:space:]]*//')
  echo "Public Key (formatted):"
  echo "$PUB_KEY"

  # Get and format private key
  echo "Preparing private key..."
  PRIVATE_KEY_HEX=$(lotus wallet export $DEFAULT_WALLET | xxd -r -p | jq -r '.PrivateKey' | base64 -d | xxd -p -c 32)
  
  # Save private key to file
  echo "Saving private key to ${CURIO_REPO_PATH}/private_key..."
  echo "$PRIVATE_KEY_HEX" > ${CURIO_REPO_PATH}/private_key
  
  echo "Importing PDP private key..."
  
  sleep 30
  
  # Import the private key using RPC
  echo "Importing private key via RPC..."
  curl -X POST -H "Content-Type: application/json" \
    -d "{\"jsonrpc\":\"2.0\",\"method\":\"CurioWeb.ImportPDPKey\",\"params\":[\"$PRIVATE_KEY_HEX\"],\"id\":1}" \
    http://${myip}:4701/api/webrpc/v0

  # Create PDP service using RPC
  echo "Creating PDP service via RPC..."
  # Escape newlines for JSON
  JSON_PUB_KEY=$(echo "$PUB_KEY" | awk '{printf "%s\\n", $0}' | sed 's/\\n$//')
  curl -X POST -H "Content-Type: application/json" \
    -d "{\"jsonrpc\":\"2.0\",\"method\":\"CurioWeb.AddPDPService\",\"params\":[\"pdp\",\"$JSON_PUB_KEY\"],\"id\":2}" \
    http://${myip}:4701/api/webrpc/v0

  # Create JWT token
  echo "Creating JWT token..."
  pdptool create-jwt-token pdp | grep -v "JWT Token:" > jwt_token.txt
  # Test connectivity to the PDP service endpoint
  echo "Testing PDP connectivity..."
  pdptool ping --service-url http://curio:80 --service-name pdp

  # Stop temporary Curio node
  echo "Stopping temporary Curio node..."
  kill -15 $CURIO_PID || kill -9 $CURIO_PID

  touch $CURIO_REPO_PATH/.init.pdp
  echo "PDP service setup complete"
fi

# Wait for .env.devnet file with all contract addresses
echo "Waiting for .env.devnet file with contract addresses..."
while [ ! -f $CURIO_REPO_PATH/.env.devnet ]; do
  echo "Waiting for .env.devnet file..."
  sleep 5
done

# Source .env.devnet to load all contract addresses
source $CURIO_REPO_PATH/.env.devnet

# Export contract addresses with names expected by curio (from curio.patch)
export CURIO_PDP_VERIFIER_ADDRESS=$PDP_VERIFIER_ADDRESS
export RECORDKEEPER_CONTRACT=$WARM_STORAGE_CONTRACT_ADDRESS
export SERVICE_REGISTRY_ADDRESS=$SP_REGISTRY_ADDRESS
# USDFC_ADDRESS is already correctly named

echo "Using contract addresses:"
echo "  PDP Verifier: $CURIO_PDP_VERIFIER_ADDRESS"
echo "  Recordkeeper (Warm Storage): $RECORDKEEPER_CONTRACT"
echo "  Service Registry: $SERVICE_REGISTRY_ADDRESS"
echo "  USDFC: $USDFC_ADDRESS"

echo Starting curio node ...
CURIO_FAKE_CPU=5 curio run --nosync --name devnet --layers seal,post,pdp-only,gui
sleep infinity