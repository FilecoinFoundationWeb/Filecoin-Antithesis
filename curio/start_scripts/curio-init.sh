#!/usr/bin/env bash
set -eu

echo "CURIO_REPO_PATH=$CURIO_REPO_PATH"
export LOTUS_PATH="${LOTUS_0_PATH}"
echo "LOTUS_PATH=$LOTUS_PATH"

echo "Waiting for lotus to be ready..."
lotus wait-api

# Wait until chain head is past epoch 5 before proceeding
MIN_BLOCKS=5
head=0
while [ "$head" -le "$MIN_BLOCKS" ]; do
    head=$(lotus chain list | awk '{print $1}' | awk -F':' '{print $1}' | tail -1)
    if [ "$head" -le "$MIN_BLOCKS" ]; then
        echo "Current head: $head, waiting for epoch > $MIN_BLOCKS..."
        sleep 1
    else
        echo "Head reached epoch $head, proceeding."
    fi
done

echo "All ready. Lets go"
myip=$(getent hosts curio | awk '{print $1}')

# Start a temporary Curio node, wait for its API, then run a callback and stop it.
# Usage: with_temporary_curio <callback_function>
with_temporary_curio() {
    local callback="$1"

    echo "Starting temporary Curio node..."
    CURIO_FAKE_CPU=5 curio run --nosync --layers seal,post,pdp-only,gui &
    local curio_pid=$!
    sleep 20

    echo "Waiting for Curio API to be ready..."
    until curio cli --machine "$myip:12300" wait-api; do
        echo "Waiting for the curio CLI to become ready..."
        sleep 5
    done

    "$callback"

    echo "Stopping temporary Curio node..."
    kill "$curio_pid" 2>/dev/null || true
    wait "$curio_pid" 2>/dev/null || true
}

if [ ! -f "$CURIO_REPO_PATH/.init.curio" ]; then
    echo "Waiting for lotus-miner to be ready..."
    lotus wait-api

    if [ ! -f "$CURIO_REPO_PATH/.init.setup" ]; then
        DEFAULT_WALLET=$(lotus wallet default)
        CURIO_WALLET=$(lotus wallet new bls)
        echo "Created new curio wallet: $CURIO_WALLET"

        FUND_CID=$(lotus send --from "$DEFAULT_WALLET" "$CURIO_WALLET" 10000 | tail -1)
        echo "Funding message CID: $FUND_CID"
        echo "Waiting for funding message to be confirmed on-chain..."
        lotus state wait-msg "$FUND_CID"
        echo "Funding confirmed."

        PDP_WALLET=$(lotus wallet new delegated)
        echo "Created PDP delegated wallet: $PDP_WALLET"

        FUND_PDP_CID=$(lotus send --from "$DEFAULT_WALLET" "$PDP_WALLET" 100 | tail -1)
        echo "Funding PDP wallet: $FUND_PDP_CID"
        lotus state wait-msg "$FUND_PDP_CID"
        echo "PDP wallet funded."

        PRIVATE_KEY_HEX=$(lotus wallet export "$PDP_WALLET" | xxd -r -p | jq -r '.PrivateKey' | base64 -d | xxd -p -c 32)
        echo "$PRIVATE_KEY_HEX" > "${CURIO_REPO_PATH}/private_key"
        echo "$PDP_WALLET" > "${CURIO_REPO_PATH}/pdp_wallet_address"
        echo "PDP private key written to ${CURIO_REPO_PATH}/private_key"

        lotus-shed miner create --deposit-margin-factor 1.01 "$CURIO_WALLET" "$CURIO_WALLET" "$CURIO_WALLET" 2KiB
        touch "$CURIO_REPO_PATH/.init.setup"
    fi

    if [ ! -f "$CURIO_REPO_PATH/.init.config" ]; then
        newminer=$(lotus state list-miners | grep -E -v 't01000|t01001' | head -1)
        echo "New Miner is $newminer"

        echo "Initiating a new Curio cluster..."
        curio config new-cluster "$newminer"

        echo "Creating market config..."
        curio config get base | sed "s/#Miners = \\[\\]/Miners = [\"${newminer}\"]/g" | curio config set --title base

        echo "Creating PDP config layer..."
        cat <<'LAYER_EOF' | curio config create --title pdp-only
[HTTP]
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
LAYER_EOF
        touch "$CURIO_REPO_PATH/.init.config"
    fi

    echo "Waiting for .env.curio file with contract addresses..."
    while [ ! -f "$CURIO_REPO_PATH/.env.curio" ]; do
        echo "Waiting for .env.curio file..."
        sleep 5
    done

    echo "Loading contract addresses from .env.curio..."
    set -a
    source "${CURIO_REPO_PATH}/.env.curio"
    set +a

    echo "Using contract addresses:"
    echo "  PDP Verifier: $CURIO_DEVNET_PDP_VERIFIER_ADDRESS"
    echo "  FWSS: $CURIO_DEVNET_FWSS_ADDRESS"
    echo "  Service Registry: $CURIO_DEVNET_SERVICE_REGISTRY_ADDRESS"
    echo "  USDFC: $CURIO_DEVNET_USDFC_ADDRESS"
    echo "  Payments: $CURIO_DEVNET_PAYMENTS_ADDRESS"
    echo "  Multicall: $CURIO_DEVNET_MULTICALL_ADDRESS"

    attach_storage() {
        curio --version
        curio cli --machine "$myip:12300" storage attach --init --seal --store "$CURIO_REPO_PATH"
    }
    with_temporary_curio attach_storage

    touch "$CURIO_REPO_PATH/.init.curio"
fi

if [ ! -f "$CURIO_REPO_PATH/.init.pdp" ]; then
    echo "Setting up PDP service..."

    setup_pdp() {
        echo "Creating PDP service secret..."
        cd "$CURIO_REPO_PATH"
        pdptool create-service-secret > pdp_service_key.txt

        PUB_KEY=$(sed -n '/Public Key:/,/-----END PUBLIC KEY-----/p' pdp_service_key.txt | grep -v "Public Key:" | sed 's/^[[:space:]]*//')
        echo "Public Key (formatted):"
        echo "$PUB_KEY"

        echo "Reading PDP private key from ${CURIO_REPO_PATH}/private_key..."
        PRIVATE_KEY_HEX=$(tr -d '[:space:]' < "${CURIO_REPO_PATH}/private_key")

        echo "Importing PDP private key..."
        sleep 10

        echo "Importing private key via RPC..."
        curl -X POST -H "Content-Type: application/json" \
            -d "{\"jsonrpc\":\"2.0\",\"method\":\"CurioWeb.ImportPDPKey\",\"params\":[\"$PRIVATE_KEY_HEX\"],\"id\":1}" \
            "http://${myip}:4701/api/webrpc/v0"

        echo "Creating PDP service via RPC..."
        JSON_PUB_KEY=$(echo "$PUB_KEY" | awk '{printf "%s\\n", $0}' | sed 's/\\n$//')
        curl -X POST -H "Content-Type: application/json" \
            -d "{\"jsonrpc\":\"2.0\",\"method\":\"CurioWeb.AddPDPService\",\"params\":[\"pdp\",\"$JSON_PUB_KEY\"],\"id\":2}" \
            "http://${myip}:4701/api/webrpc/v0"

        echo "Creating JWT token..."
        pdptool create-jwt-token pdp | grep -v "JWT Token:" > jwt_token.txt

        echo "Testing PDP connectivity..."
        pdptool ping --service-url http://curio:80 --service-name pdp
    }
    with_temporary_curio setup_pdp

    touch "$CURIO_REPO_PATH/.init.pdp"
    echo "PDP service setup complete"
fi

echo "Starting curio node..."
CURIO_FAKE_CPU=5 curio run --nosync --name devnet --layers seal,post,pdp-only,gui
sleep infinity
