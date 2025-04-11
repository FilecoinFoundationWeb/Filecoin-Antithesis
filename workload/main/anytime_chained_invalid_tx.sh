#!/bin/bash

APP_BINARY="/opt/antithesis/app"
CONFIG_FILE="/opt/antithesis/resources/config.json"
OPERATION="chainedInvalidTx"
NODE_NAMES=("Lotus1" "Lotus2")
TX_COUNT=50

if [ ! -f "$APP_BINARY" ]; then
    echo "Error: $APP_BINARY not found."
    exit 1
fi

if [ ! -f "$CONFIG_FILE" ]; then
    echo "Error: $CONFIG_FILE not found."
    exit 1
fi

# Randomly select a node to run the test on
random_index=$((RANDOM % ${#NODE_NAMES[@]}))
random_node=${NODE_NAMES[$random_index]}

echo "Running chained invalid transactions on node: $random_node"
echo "Transaction count: $TX_COUNT"

# Run the operation
$APP_BINARY -operation "$OPERATION" -node "$random_node" -count "$TX_COUNT" -config "$CONFIG_FILE"

exit_code=$?
if [ $exit_code -ne 0 ]; then
    echo "Operation failed with exit code: $exit_code"
    exit $exit_code
fi

echo "Chained invalid transactions operation completed successfully." 