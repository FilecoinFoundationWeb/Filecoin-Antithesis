#!/bin/bash
set -e

echo "Running Fuzz Test: FuzzIdentifyPushToLotus"


# Read the multiaddr from the file.
LOTUS_1_TARGET=$(cat "/root/devgen/lotus-1/lotus-1-ipv4addr")
LOTUS_2_TARGET=$(cat "/root/devgen/lotus-2/lotus-2-ipv4addr")

# Randomly select a Lotus target from the available options.
random_targets=("LOTUS_1_TARGET" "LOTUS_2_TARGET")
selected_target=${random_targets[$((RANDOM % ${#random_targets[@]}))]}
export LOTUS_TARGET=${!selected_target}

echo "LOTUS_TARGET set to: $LOTUS_TARGET"

# Run the fuzz test for 30 seconds.
go run /opt/antithesis/go-test-scripts/identify.go

echo "Fuzz test completed."