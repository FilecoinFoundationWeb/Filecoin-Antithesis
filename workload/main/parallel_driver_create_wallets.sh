#!/bin/bash

WORKLOAD="/opt/antithesis/workload"
CONFIG_FILE="/opt/antithesis/resources/config.json"
NODE_NAMES=("Lotus0" "Lotus1" "Forest0")
MIN_WALLETS=2
MAX_WALLETS=5

# Ensure the workload binary exists
if [ ! -f "$WORKLOAD" ]; then
    echo "Error: $WORKLOAD not found."
    exit 1
fi

# Ensure the config file exists
if [ ! -f "$CONFIG_FILE" ]; then
    echo "Error: $CONFIG_FILE not found."
    exit 1
fi

# Create random wallets on all nodes
random_wallet_count=$((RANDOM % (MAX_WALLETS - MIN_WALLETS + 1) + MIN_WALLETS))

echo "Creating $random_wallet_count wallets on each node"
for node in "${NODE_NAMES[@]}"; do
    echo "Creating wallets on $node..."
    $WORKLOAD wallet create --node "$node" --count "$random_wallet_count"
done

echo "Wallet creation workload completed."
