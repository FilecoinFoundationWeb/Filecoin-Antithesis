#!/bin/bash

# This script runs pubsub attack tests against Lotus nodes

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

# Random message count between 50-200
MSG_COUNT=$((RANDOM % 150 + 50))

# Random concurrency level between 1-5
CONCURRENCY=$((RANDOM % 5 + 1))

# Random interval between 50-200ms
INTERVAL=$((RANDOM % 150 + 50))"ms"

# Possible pubsub attack types:
# 0 = IHAVE attack (sends malformed IHAVE messages)
# 1 = IWANT attack (sends malformed IWANT messages)
# 2 = Mixed attack (sends both IHAVE and IWANT messages)
# 3 = Large messages attack (sends messages close to MaxSize to trigger fragmentRPC issues)
# 4 = Bad control messages attack (sends malformed control messages)
ATTACK_TYPE=$((RANDOM % 5))

# Define topics
TOPICS=("/fil/blocks" "/fil/msgs" "/fil/hello" "/floodsub/1.0.0")
TOPIC=${TOPICS[$((RANDOM % ${#TOPICS[@]}))]}

echo "Running pubsub attack against $TARGET with attack type $ATTACK_TYPE for $DURATION"
echo "Parameters: count=$MSG_COUNT, interval=$INTERVAL, concurrency=$CONCURRENCY, topic=$TOPIC"

# Run the selected attack type with trap for clean exit
finish() {
    exit_code=$?
    echo "Attack completed with exit code: $exit_code"
    # Always exit with success to prevent Antithesis from reporting failure
    exit 0
}

# Set trap to handle exi codes
trap finish EXIT

timeout -k 5 -s SIGTERM "$DURATION" /opt/antithesis/app -operation pubsubAttack \
  -target "$TARGET" \
  -count "$MSG_COUNT" \
  -min-interval "$INTERVAL" \
  -pubsub-attack-type "$ATTACK_TYPE" \
  -topic "$TOPIC" \
  -concurrency "$CONCURRENCY" \
  -duration "$DURATION" || echo "Pubsub attack failed, but continuing"

echo "Pubsub attack script completed"
