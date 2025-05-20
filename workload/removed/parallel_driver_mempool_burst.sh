#!/bin/bash

APP_BINARY="/opt/antithesis/app"
CONFIG_FILE="/opt/antithesis/resources/config.json"
OPERATION="mempoolFuzz"

NODE_NAMES=("Lotus1" "Lotus2")
random_index=$((RANDOM % ${#NODE_NAMES[@]}))
NODE=${NODE:-${NODE_NAMES[$random_index]}}

COUNT=${COUNT:-$((RANDOM % 150 + 100))}
CONCURRENCY=${CONCURRENCY:-$((RANDOM % 4 + 2))}
DURATION="30s"  

if [ ! -f "$APP_BINARY" ]; then
    echo "Error: $APP_BINARY not found."
    exit 1
fi

if [ ! -f "$CONFIG_FILE" ]; then
    echo "Error: $CONFIG_FILE not found."
    exit 1
fi

echo "[INFO] Starting burst mempool fuzzing"
echo "[INFO] Node: $NODE, Count: $COUNT, Concurrency: $CONCURRENCY"

$APP_BINARY -operation "$OPERATION" \
    -node "$NODE" \
    -count "$COUNT" \
    -concurrency "$CONCURRENCY" \
    -config "$CONFIG_FILE" \
    -duration "$DURATION"

exit_code=$?
if [ $exit_code -ne 0 ]; then
    echo "[ERROR] Operation failed with exit code: $exit_code"
    exit $exit_code
fi

echo "[INFO] Burst mempool fuzzing completed successfully." 