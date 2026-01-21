#!/bin/bash

ENV_FILE="/opt/antithesis/synapse-sdk/.env.localnet"
source $ENV_FILE


PRIVATE_KEY="$CLIENT_PRIVATE_KEY" \
RPC_URL="$LOCALNET_RPC_URL" \
WARM_STORAGE_ADDRESS="$LOCALNET_WARM_STORAGE_CONTRACT_ADDRESS" \
MULTICALL3_ADDRESS="$LOCALNET_MULTICALL3_ADDRESS" \
node --env-file="$ENV_FILE" \
    /opt/antithesis/synapse-sdk/utils/example-storage-e2e.js \
    /opt/antithesis/synapse-sdk/README.md