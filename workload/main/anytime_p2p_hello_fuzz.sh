#!/bin/bash
set -e

echo "Running Fuzz Test: FuzzHelloToLotus"

# Read the multiaddr from the files.
LOTUS_1_TARGET=$(cat "/root/devgen/lotus-1/lotus-1-ipv4addr")
LOTUS_2_TARGET=$(cat "/root/devgen/lotus-2/lotus-2-ipv4addr")
FOREST_TARGET=$(cat "/root/devgen/forest/forest-listen-addr")

# Randomly select a Lotus target from the available options.
random_targets=("FOREST_TARGET")
selected_target=${random_targets[$((RANDOM % ${#random_targets[@]}))]}
export LOTUS_TARGET=${!selected_target}

echo "LOTUS_TARGET set to: $LOTUS_TARGET"

# Run the fuzz test for 30 seconds.
go test -v -fuzz=FuzzHello -fuzztime=30s /opt/antithesis/go-test-scripts/p2p_hello_test.go

echo "Fuzz test completed."
