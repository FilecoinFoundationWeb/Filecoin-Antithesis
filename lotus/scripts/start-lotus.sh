#!/bin/bash

no="$1"

lotus_data_dir="LOTUS_${no}_DATA_DIR"
export LOTUS_DATA_DIR="${!lotus_data_dir}"

export LOTUS_CHAININDEXER_ENABLEINDEXER=true

# required via docs:
lotus_path="LOTUS_${no}_PATH"
export LOTUS_PATH="${!lotus_path}"

lotus_miner_path="LOTUS_MINER_${no}_PATH"
export LOTUS_MINER_PATH="${!lotus_miner_path}"

export LOTUS_RPC_PORT=$LOTUS_RPC_PORT
export LOTUS_SKIP_GENESIS_CHECK=${LOTUS_SKIP_GENESIS_CHECK}
export CGO_CFLAGS_ALLOW="-D__BLST_PORTABLE__"
export CGO_CFLAGS="-D__BLST_PORTABLE__"

# ==============================================================================
# RESTART FIX: Track restart count for monitoring
# ==============================================================================
RESTART_COUNT_FILE="${LOTUS_DATA_DIR}/.restart_count"
RESTART_COUNT=0

if [ -f "$RESTART_COUNT_FILE" ]; then
    RESTART_COUNT=$(cat "$RESTART_COUNT_FILE")
fi
RESTART_COUNT=$((RESTART_COUNT + 1))
echo $RESTART_COUNT > "$RESTART_COUNT_FILE"

echo "====================================================================="
echo "lotus${no}: Node restart #${RESTART_COUNT}"
echo "====================================================================="

if [ $RESTART_COUNT -gt 10 ]; then
    echo "⚠️  WARNING: lotus${no} has restarted $RESTART_COUNT times"
fi
# ==============================================================================

if [ ! -f "${LOTUS_DATA_DIR}/config.toml" ]; then
    INIT_MODE=true
else
    INIT_MODE=false
fi

# ==============================================================================
# RESTART FIX: Clean up stale daemon processes before starting
# ==============================================================================
echo "lotus${no}: Checking for stale daemon processes..."

if pgrep -x "lotus" > /dev/null; then
    echo "lotus${no}: Found running lotus daemon, cleaning up..."
    pkill -9 -x "lotus"
    sleep 2
fi

# Clean stale lock files
if [ -f "${LOTUS_PATH}/datastore/.lock" ]; then
    echo "lotus${no}: Removing stale lock file"
    rm -f "${LOTUS_PATH}/datastore/.lock"
fi

echo "lotus${no}: Cleanup complete"
# ==============================================================================

# ==============================================================================
# RESTART FIX: Cache drand info, don't refetch on every restart
# ==============================================================================
CHAIN_INFO_FILE="chain_info"

if [ -f "$CHAIN_INFO_FILE" ] && [ -s "$CHAIN_INFO_FILE" ]; then
    echo "lotus${no}: Using cached drand chain info"
    export DRAND_CHAIN_INFO=$(pwd)/$CHAIN_INFO_FILE
else
    echo "lotus${no}: Fetching drand chain info from drand0..."
    FETCH_RETRIES=0
    MAX_FETCH_RETRIES=30

    while true; do
        response=$(curl -s --fail "http://drand0/info" 2>&1)

        if [ $? -eq 0 ] && echo "$response" | jq -e '.public_key?' >/dev/null 2>&1; then
            echo "$response" | jq -c > "${CHAIN_INFO_FILE}.tmp"
            mv "${CHAIN_INFO_FILE}.tmp" "$CHAIN_INFO_FILE"
            echo "$response"
            export DRAND_CHAIN_INFO=$(pwd)/$CHAIN_INFO_FILE
            echo "lotus${no}: Drand chain info ready"
            break
        else
            FETCH_RETRIES=$((FETCH_RETRIES + 1))
            if [ $FETCH_RETRIES -ge $MAX_FETCH_RETRIES ]; then
                echo "ERROR: lotus${no}: Failed to fetch drand info after $MAX_FETCH_RETRIES attempts"
                exit 1
            fi
            echo "lotus${no}: Drand fetch failed (attempt $FETCH_RETRIES/$MAX_FETCH_RETRIES), retrying..."
            sleep 2
        fi
    done
fi
# ==============================================================================

if [ "$INIT_MODE" = "true" ]; then
    echo "lotus${no}: INIT MODE - First run detected"
    host_ip=$(getent hosts "lotus${no}" | awk '{ print $1 }')

    echo "---------------------------"
    echo "ip address: $host_ip"
    echo "---------------------------"

    # ===========================================================================
    # FIX: Write config.toml to LOTUS_DATA_DIR, not current directory
    # This ensures INIT_MODE detection works correctly on restart
    # ===========================================================================
    sed "s|\${host_ip}|$host_ip|g; s|\${LOTUS_RPC_PORT}|$LOTUS_RPC_PORT|g" config.toml.template > ${LOTUS_DATA_DIR}/config.toml
    echo "lotus${no}: Created config at ${LOTUS_DATA_DIR}/config.toml"

    if [ "$no" -eq 0 ]; then
        ./scripts/setup-genesis.sh
    fi

    cat ${SHARED_CONFIGS}/localnet.json | jq -r '.NetworkName' > ${LOTUS_DATA_DIR}/network_name

    if [ "$no" -eq 0 ]; then
        # Node 0: Generate genesis on first run
        # TODO: This step is FLAKY!
        # The error message we see is the following:
        #
        # genesis func failed: make genesis block: failed to verify presealed data: failed to create verifier: failed to call method: message failed with backtrace:
        # 00: f06 (method 2) -- Allowance 0 below minimum deal size for add verifier f081 (16)
        #
        # Is there a way to resolve this?
        lotus --repo="${LOTUS_PATH}" daemon --lotus-make-genesis=${SHARED_CONFIGS}/devgen.car --genesis-template=${SHARED_CONFIGS}/localnet.json --bootstrap=false --config=${LOTUS_DATA_DIR}/config.toml&
    else
        # Other nodes: Use existing genesis
        lotus --repo="${LOTUS_PATH}" daemon --genesis=${SHARED_CONFIGS}/devgen.car --bootstrap=false --config=${LOTUS_DATA_DIR}/config.toml&
    fi
else
    # ===========================================================================
    # RESTART FIX: On restart, all nodes (including node0) use existing genesis
    # Genesis was already created on first run, just load it
    # ===========================================================================
    echo "lotus${no}: Restart mode - using existing genesis"
    lotus --repo="${LOTUS_PATH}" daemon --genesis=${SHARED_CONFIGS}/devgen.car --bootstrap=false --config=${LOTUS_DATA_DIR}/config.toml&
fi

# ==============================================================================
# RESTART FIX: Verify daemon actually started successfully
# ==============================================================================
DAEMON_PID=$!
echo "lotus${no}: Daemon started with PID $DAEMON_PID"
sleep 3

if ! kill -0 $DAEMON_PID 2>/dev/null; then
    echo "ERROR: lotus${no}: Daemon process $DAEMON_PID exited unexpectedly"
    echo "Check logs for startup errors"
    exit 1
fi
echo "lotus${no}: Daemon process verified running"
# ==============================================================================

lotus --version
lotus wait-api

lotus net listen | grep -v "127.0.0.1" | grep -v "::1" | head -n 1 > ${LOTUS_DATA_DIR}/lotus${no}-ipv4addr
lotus net id > ${LOTUS_DATA_DIR}/lotus${no}-p2pID
if [ ! -f "${LOTUS_DATA_DIR}/lotus${no}-jwt" ]; then
    lotus auth create-token --perm admin > ${LOTUS_DATA_DIR}/lotus${no}-jwt
fi

# connecting to peers
connect_with_retries() {
    local retries=10
    local addr_file="$1"
    
    for (( j=1; j<=retries; j++ )); do
        echo "attempt $j..."

        ip=$(<"$addr_file")
        if lotus net connect "$ip"; then
            echo "successful connect!"
            return 0
        else
            sleep 2
        fi
    done

    echo "ERROR: reached $MAX_RETRIES attempts."
    return 1
}

echo "connecting to other lotus nodes..."
for (( i=0; i<$NUM_LOTUS_CLIENTS; i++ )); do
    if [[ $i -eq $no ]]; then
        continue
    fi

    other_lotus_data_dir="LOTUS_${i}_DATA_DIR"
    OTHER_LOTUS_DATA_DIR="${!other_lotus_data_dir}"
    addr_file="${OTHER_LOTUS_DATA_DIR}/lotus${i}-ipv4addr"

    echo "Connecting to lotus$i at $addr_file"
    connect_with_retries "$addr_file"
done

echo "connecting to forest nodes..."
for (( i=0; i<$NUM_FOREST_CLIENTS; i++ )); do
    forest_data_dir="FOREST_${i}_DATA_DIR"
    FOREST_DATA_DIR="${!forest_data_dir}"
    addr_file="${FOREST_DATA_DIR}/forest${i}-ipv4addr"

    echo "Connecting to forest$i at $addr_file"
    connect_with_retries "$addr_file"
done

echo "lotus${no}: completed startup"

sleep infinity
