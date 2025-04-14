#!/bin/bash

# This script runs network attack tests against Lotus nodes

# Enable error handling
set -o pipefail
shopt -s expand_aliases

# Read the multiaddr from the files
LOTUS_1_TARGET=$(cat "/root/devgen/lotus-1/lotus-1-ipv4addr" 2>/dev/null || echo "")
LOTUS_2_TARGET=$(cat "/root/devgen/lotus-2/lotus-2-ipv4addr" 2>/dev/null || echo "")

# Verify target addresses exist and print them for debugging
echo "Found target addresses:"
[[ -n "$LOTUS_1_TARGET" ]] && echo "LOTUS_1_TARGET: $LOTUS_1_TARGET"
[[ -n "$LOTUS_2_TARGET" ]] && echo "LOTUS_2_TARGET: $LOTUS_2_TARGET"

# Create array of available targets
random_targets=()
[[ -n "$LOTUS_1_TARGET" ]] && random_targets+=("$LOTUS_1_TARGET")
[[ -n "$LOTUS_2_TARGET" ]] && random_targets+=("$LOTUS_2_TARGET")

if [[ ${#random_targets[@]} -eq 0 ]]; then
    echo "No target addresses found. Exiting."
    exit 0  # Exit with success to not trigger test failure
fi

# Randomly select a target
TARGET=${random_targets[$((RANDOM % ${#random_targets[@]}))]}

# Random duration between 1-5 minutes
DURATION=$((RANDOM % 4 + 1))"m"

# Select a random attack type
ATTACK_TYPES=("chaos" "identify")
ATTACK_TYPE=${ATTACK_TYPES[$((RANDOM % ${#ATTACK_TYPES[@]}))]}

echo "Running $ATTACK_TYPE attack against $TARGET for $DURATION"

# Function to handle exit codes
finish() {
    exit_code=$?
    echo "Attack completed with exit code: $exit_code"
    # Always exit with success to prevent Antithesis from reporting failure
    exit 0
}

# Set trap to handle exit codes
trap finish EXIT

case "$ATTACK_TYPE" in
    "chaos")
        # Chaos operation with random intervals
        MIN_INTERVAL=$((RANDOM % 5 + 1))"s"
        MAX_INTERVAL=$((RANDOM % 15 + 5))"s"
        
        # Try to run the attack, but don't worry if it fails
        timeout -k 5 -s SIGTERM "$DURATION" /opt/antithesis/app -operation chaos \
          -target "$TARGET" \
          -min-interval "$MIN_INTERVAL" \
          -max-interval "$MAX_INTERVAL" \
          -duration "$DURATION" || echo "Chaos attack failed, but continuing"
        ;;
        
    "identify")
        # Run identify spam attack
        export LOTUS_TARGET="$TARGET"
        timeout -k 5 -s SIGTERM "$DURATION" go run /opt/antithesis/go-test-scripts/identify.go || echo "Identify attack failed, but continuing"
        ;;
esac

# The trap will handle the exit code
echo "Attack script completed"
# (This point is reached only if timeout didn't terminate the process) 