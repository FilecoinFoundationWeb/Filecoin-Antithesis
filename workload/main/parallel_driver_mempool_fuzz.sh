#!/bin/bash

APP_BINARY="/opt/antithesis/app"
CONFIG_FILE="/opt/antithesis/resources/config.json"
OPERATION="mempoolFuzz"

NODE_NAMES=("Lotus1" "Lotus2")
random_index=$((RANDOM % ${#NODE_NAMES[@]}))
NODE=${NODE_NAMES[$random_index]}

COUNT=50
CONCURRENCY=$((RANDOM % 5 + 3))

if [ ! -f "$APP_BINARY" ]; then
    echo "Error: $APP_BINARY not found."
    exit 1
fi

if [ ! -f "$CONFIG_FILE" ]; then
    echo "Error: $CONFIG_FILE not found."
    exit 1
fi

$APP_BINARY -operation "$OPERATION" \
    -node "$NODE" \
    -count "$COUNT" \
    -concurrency "$CONCURRENCY" \
    -config "$CONFIG_FILE"

exit_code=$?
if [ $exit_code -ne 0 ]; then
    echo "Operation failed with exit code: $exit_code"
    exit $exit_code
fi

echo "Mempool fuzzing operation completed successfully." 