#!/bin/bash

# This script runs a chaotic test scenario against the Filecoin nodes.
# It starts background processes for network chaos and P2P fuzzing,
# then enters a loop to perform various disruptive actions and assertions.

# Exit immediately if a command exits with a non-zero status.
set -e

# --- Configuration ---
# The total duration for the test loop (e.g., "30m" for 30 minutes)
TEST_DURATION="30m" 
# The duration for each loop iteration (in seconds)
SLEEP_INTERVAL=60 

# Multiaddresses for the target nodes.
# You can typically find these in your docker-compose logs when the nodes start up.
# They look like: /ip4/10.20.20.24/tcp/1347/p2p/12D3Koo...
TARGET_ADDR_LOTUS_1="<MULTIADDR_OF_LOTUS_NODE_1>"
TARGET_ADDR_LOTUS_2="<MULTIADDR_OF_LOTUS_NODE_2>"

# Check for placeholder values
if [[ "$TARGET_ADDR_LOTUS_1" == "<MULTIADDR_OF_LOTUS_NODE_1>" || "$TARGET_ADDR_LOTUS_2" == "<MULTIADDR_OF_LOTUS_NODE_2>" ]]; then
    echo "[ERROR] Please replace the placeholder multiaddresses in this script with the actual P2P addresses of your Lotus nodes."
    exit 1
fi

# --- Helper Functions ---
cleanup() {
    echo ""
    echo "[INFO] Cleaning up background processes..."
    # The '|| true' ensures that the script doesn't exit if the process is already gone
    kill $CHAOS_PID || true
    kill $PING_PID || true
    echo "[INFO] Cleanup complete."
}

# Register the cleanup function to be called on script exit
trap cleanup EXIT

# --- Main Test Execution ---

echo "[INFO] --- Initial Setup: Ensuring Wallets Exist ---"
# Ensure all nodes have wallets to prevent startup issues
/opt/antithesis/workload/main -operation create -node Lotus1
/opt/antithesis/workload/main -operation create -node Lotus2
echo "[INFO] Wallet setup complete."

echo ""
echo "[INFO] --- Starting Background Chaos Operations for ${TEST_DURATION} ---"

# Start network chaos (e.g., partitioning) against lotus-1 in the background
echo "[INFO] Starting network chaos against $TARGET_ADDR_LOTUS_1"
/opt/antithesis/workload/main -operation chaos -target "$TARGET_ADDR_LOTUS_1" -duration "$TEST_DURATION" &
CHAOS_PID=$!
echo "[INFO] Chaos process started with PID $CHAOS_PID"

# Start P2P ping attack against lotus-2 in the background
echo "[INFO] Starting P2P ping attack against $TARGET_ADDR_LOTUS_2"
/opt/antithesis/workload/main -operation pingAttack -target "$TARGET_ADDR_LOTUS_2" -duration "$TEST_DURATION" &
PING_PID=$!
echo "[INFO] Ping attack process started with PID $PING_PID"

echo ""
echo "[INFO] --- Running Main Test Loop ---"
test_end=$((SECONDS + $(echo "$TEST_DURATION" | sed -e 's/m/ * 60/' -e 's/s//' | bc)))

while [ $SECONDS -lt $test_end ]; do
    iteration=$((iteration + 1))
    echo ""
    echo "[INFO] --- Loop Iteration $iteration ---"

    # Pick a random disruptive operation to run
    op_num=$((1 + RANDOM % 5))
    case $op_num in
        1)
            echo "[INFO] Running operation: sendConsensusFault"
            /opt/antithesis/workload/main -operation sendConsensusFault
            ;;
        2)
            echo "[INFO] Running operation: blockfuzz on Lotus1"
            /opt/antithesis/workload/main -operation blockfuzz -node Lotus1
            ;;
        3)
            echo "[INFO] Running operation: mempoolFuzz on Lotus2"
            /opt/antithesis/workload/main -operation mempoolFuzz -node Lotus2 -count 50
            ;;
        4)
            echo "[INFO] Running operation: Disconnect/Reconnect on Lotus1"
            /opt/antithesis/workload/main -operation connect -node Lotus1
            ;;
        5)
            echo "[INFO] Running operation: spam"
            /opt/antithesis/workload/main -operation spam
            ;;
    esac

    # Run assertions after each disruptive operation
    echo ""
    echo "[INFO] --- Running Assertions (Iteration $iteration) ---"
    /opt/antithesis/workload/main -operation checkConsensus
    /opt/antithesis/workload/main -operation stateMismatch -node Lotus1
    /opt/antithesis/workload/main -operation stateMismatch -node Lotus2
    /opt/antithesis/workload/main -operation checkBackfill

    # Sleep for a bit before the next iteration
    echo ""
    echo "[INFO] Sleeping for $SLEEP_INTERVAL seconds..."
    sleep $SLEEP_INTERVAL
done

echo ""
echo "[INFO] --- Test Loop Finished ---"
# The cleanup function will be called automatically on exit

echo "[INFO] --- Final Sanity Check ---"
/opt/antithesis/workload/main -operation checkConsensus

echo ""
echo "[INFO] --- Test Scenario Complete ---" 