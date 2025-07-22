#!/bin/bash

echo "Starting anytime P2P bomb"

# Run the P2P bomb
/opt/antithesis/app stress p2p-bomb --node "Lotus1"

echo "P2P bomb completed"