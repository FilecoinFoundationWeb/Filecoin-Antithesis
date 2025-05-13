#!/bin/bash

APP_BINARY="/opt/antithesis/app"
CONFIG_FILE="/opt/antithesis/resources/config.json"
OPERATION="mempoolFuzz"

NODE_NAMES=("Lotus1" "Lotus2")
random_index=$((RANDOM % ${#NODE_NAMES[@]}))
NODE=${NODE:-${NODE_NAMES[$random_index]}}

COUNT=${COUNT:-$((RANDOM % 120 + 100))}
CONCURRENCY=${CONCURRENCY:-$((RANDOM % 3 + 2))}
DURATION="120s"  

$APP_BINARY -operation "$OPERATION" \
    -node "$NODE" \
    -count "$COUNT" \
    -concurrency "$CONCURRENCY" \
    -config "$CONFIG_FILE" \
    -duration "$DURATION"

exit_code=$?
if [ $exit_code -ne 0 ]; then
    echo "[WARNING] Operation completed with non-zero exit code: $exit_code (suppressing error)"
fi

echo "[INFO] Edge case mempool fuzzing completed." 
exit 0 