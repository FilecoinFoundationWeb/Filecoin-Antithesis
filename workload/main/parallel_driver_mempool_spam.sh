#!/bin/bash

export FILECOIN_RPC="http://lotus-1:1234/rpc/v1" 
export FILECOIN_TOKEN=$(cat /root/devgen/lotus-1/jwt)

# Configurable via environment variables with sensible defaults
COUNT="${COUNT:-100}"
AMOUNT="${AMOUNT:-0.1}"
CONCURRENT="${CONCURRENT:-2}"
MIN_BALANCE="${MIN_BALANCE:-1}"
REFILL_AMOUNT="${REFILL_AMOUNT:-10}"
AUTO_CREATE_WALLETS="${AUTO_CREATE_WALLETS:-true}"

# Build command with all flags
CMD="filwizard mempool spam --count $COUNT --amount $AMOUNT --concurrent $CONCURRENT --min-balance $MIN_BALANCE --refill-amount $REFILL_AMOUNT --wait"

eval "$CMD"

