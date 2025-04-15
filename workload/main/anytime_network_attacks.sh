#!/bin/bash
set -e

# This script runs network attack tests against Lotus nodes

LOTUS_1_TARGET=$(cat "/root/devgen/lotus-1/lotus-1-ipv4addr" 2>/dev/null || echo "")
LOTUS_2_TARGET=$(cat "/root/devgen/lotus-2/lotus-2-ipv4addr" 2>/dev/null || echo "")

random_targets=()
[[ -n "$LOTUS_1_TARGET" ]] && random_targets+=("$LOTUS_1_TARGET")
[[ -n "$LOTUS_2_TARGET" ]] && random_targets+=("$LOTUS_2_TARGET")

if [[ ${#random_targets[@]} -eq 0 ]]; then
    echo "No target addresses found. Exiting."
    exit 1
fi

# Randomly select a target
TARGET=${random_targets[$((RANDOM % ${#random_targets[@]}))]}

# Random duration between 1-5 minutes
DURATION=$((RANDOM % 4 + 1))"m"

ATTACK_TYPES=("chaos" "identify")
ATTACK_TYPE=${ATTACK_TYPES[$((RANDOM % ${#ATTACK_TYPES[@]}))]}

echo "Running $ATTACK_TYPE attack against $TARGET for $DURATION"

# Run the appropriate attack
if [[ "$ATTACK_TYPE" == "chaos" ]]; then
    # Chaos operation with random intervals
    MIN_INTERVAL=$((RANDOM % 5 + 1))"s"
    MAX_INTERVAL=$((RANDOM % 15 + 5))"s"
    
    /opt/antithesis/app -operation chaos \
      -target "$TARGET" \
      -min-interval "$MIN_INTERVAL" \
      -max-interval "$MAX_INTERVAL" \
      -duration "$DURATION"
    
elif [[ "$ATTACK_TYPE" == "identify" ]]; then
    export LOTUS_TARGET="$TARGET"
    go run /opt/antithesis/go-test-scripts/identify.go
fi

echo "Attack completed successfully"
exit 0
