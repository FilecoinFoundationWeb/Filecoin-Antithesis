#!/bin/bash

APP_BINARY="/opt/antithesis/app"
CONFIG_FILE="/opt/antithesis/resources/config.json"
OPERATION="pingAttack"

PING_ATTACK_TYPES=("random" "oversized" "empty" "multiple" "incomplete")
RANDOM_TYPE=${PING_ATTACK_TYPES[$((RANDOM % ${#PING_ATTACK_TYPES[@]}))]}
CONCURRENCY=$((RANDOM % 10 + 5))  
DURATION=$((RANDOM % 30 + 30))"s"  
MIN_INTERVAL="100ms"
MAX_INTERVAL="500ms"

LOTUS_1_TARGET=$(cat "/root/devgen/lotus-1/lotus-1-ipv4addr" 2>/dev/null || echo "")
LOTUS_2_TARGET=$(cat "/root/devgen/lotus-2/lotus-2-ipv4addr" 2>/dev/null || echo "")

targets=()
[[ -n "$LOTUS_1_TARGET" ]] && targets+=("$LOTUS_1_TARGET")
[[ -n "$LOTUS_2_TARGET" ]] && targets+=("$LOTUS_2_TARGET")

random_index=$((RANDOM % ${#targets[@]}))
TARGET=${targets[$random_index]}

echo "Running ping attack against target: $TARGET"
echo "Attack type: $RANDOM_TYPE"
echo "Concurrency: $CONCURRENCY"
echo "Duration: $DURATION"
echo "Intervals: $MIN_INTERVAL - $MAX_INTERVAL"

$APP_BINARY -operation "$OPERATION" \
    -target "$TARGET" \
    -ping-attack-type "$RANDOM_TYPE" \
    -concurrency "$CONCURRENCY" \
    -min-interval "$MIN_INTERVAL" \
    -max-interval "$MAX_INTERVAL" \
    -duration "$DURATION" \
    -config "$CONFIG_FILE"

exit_code=$?
if [ $exit_code -ne 0 ]; then
    echo "Operation failed with exit code: $exit_code"
    exit $exit_code
fi

echo "Ping attack operation completed successfully."