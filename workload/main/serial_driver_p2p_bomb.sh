#!/bin/bash

NODE_NAMES=("Lotus1" "Lotus2" "Forest")

echo "Starting anytime P2P bomb"

# Run the P2P bomb
for node in "${NODE_NAMES[@]}"; do
    /opt/antithesis/app stress p2p-bomb --node "$node"
done

echo "P2P bomb completed"