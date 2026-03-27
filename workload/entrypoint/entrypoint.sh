#!/bin/bash
set -e

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log_info() { echo -e "${GREEN}[WORKLOAD]${NC} $1"; }
log_warn() { echo -e "${YELLOW}[WORKLOAD]${NC} $1"; }

# ── 1. Generate genesis wallets ──
log_info "Generating pre-funded genesis wallets..."
/opt/antithesis/genesis-prep --count 100 --out /shared/configs
log_info "Genesis wallet generation complete."

# ── 2. Time sync ──
log_info "Synchronizing system time..."
if ntpdate -q pool.ntp.org &>/dev/null; then
    ntpdate -u pool.ntp.org || log_warn "Time sync failed."
else
    log_warn "Unable to query NTP servers."
fi
log_info "System time: $(date -u '+%Y-%m-%d %H:%M:%S UTC')"

# ── 3. Wait for blockchain to reach minimum epoch ──
WAIT_HEIGHT="${STRESS_WAIT_HEIGHT:-10}"
RPC_URL="http://lotus0:${STRESS_RPC_PORT:-1234}/rpc/v1"
log_info "Waiting for block height to reach ${WAIT_HEIGHT}..."
while true; do
    height=$(curl -sf -X POST -H "Content-Type: application/json" \
        --data '{"jsonrpc":"2.0","method":"Filecoin.ChainHead","params":[],"id":1}' \
        "$RPC_URL" 2>/dev/null | jq -r '.result.Height // empty' 2>/dev/null)
    if [ -n "$height" ] && [ "$height" -ge "$WAIT_HEIGHT" ] 2>/dev/null; then
        log_info "Blockchain ready at height ${height}"
        break
    fi
    log_info "Current height: ${height:-unknown}, waiting..."
    sleep 5
done

# ── 4. Wait for all miners to have produced at least one block ──
log_info "Discovering registered miners via StateListMiners..."
while true; do
    MINER_ADDRS=$(curl -sf -m 5 -X POST -H "Content-Type: application/json" \
        --data '{"jsonrpc":"2.0","method":"Filecoin.StateListMiners","params":[null],"id":1}' \
        "$RPC_URL" 2>/dev/null | jq -rc '.result // empty' 2>/dev/null)
    if [ -n "$MINER_ADDRS" ] && [ "$MINER_ADDRS" != "null" ] && [ "$MINER_ADDRS" != "[]" ]; then
        break
    fi
    sleep 3
done
MINER_COUNT=$(echo "$MINER_ADDRS" | jq 'length')
log_info "Found ${MINER_COUNT} miners on-chain: $(echo "$MINER_ADDRS" | jq -r 'join(", ")')"

# Wait for each miner daemon's RPC to respond (requires LOTUS_API_LISTENADDRESS fix)
MINER_RPC_PORT="${LOTUS_MINER_RPC_PORT:-2345}"
for i in $(seq 0 $((MINER_COUNT - 1))); do
    miner="lotus-miner${i}"
    MINER_URL="http://${miner}:${MINER_RPC_PORT}/rpc/v0"
    log_info "Waiting for ${miner} API..."
    while true; do
        ver=$(curl -sf -m 5 -X POST -H "Content-Type: application/json" \
            --data '{"jsonrpc":"2.0","method":"Filecoin.Version","params":[],"id":1}' \
            "$MINER_URL" 2>/dev/null | jq -r '.result.Version // empty' 2>/dev/null)
        if [ -n "$ver" ]; then
            log_info "${miner} ready (version=${ver})"
            break
        fi
        sleep 3
    done
done
log_info "All ${MINER_COUNT} miners confirmed reachable."

# ── 5. Wait for filwizard if running (FOC profile, auto-detected via DNS) ──
ENV_FILE="/shared/environment.env"
FILWIZARD_READY="/shared/filwizard_ready"
if getent hosts filwizard &>/dev/null; then
    log_info "FOC profile detected (filwizard reachable)"

    log_info "Waiting for environment.env..."
    while [ ! -f "$ENV_FILE" ] || [ ! -s "$ENV_FILE" ]; do sleep 2; done
    log_info "environment.env ready"

    log_info "Waiting for filwizard SP registration to complete..."
    while [ ! -f "$FILWIZARD_READY" ]; do sleep 2; done
    log_info "Filwizard setup complete (SP registered)"

    # Source it (for any scripts that need vars)
    set -a
    source "$ENV_FILE"
    set +a
else
    log_info "Non-FOC profile."
fi

# ── 6. Signal setup complete to Antithesis ──
log_info "All prerequisites met, signaling setup complete to Antithesis..."
/opt/antithesis/setup-complete

# ── 7. Launch FOC sidecar if in FOC profile ──
if getent hosts filwizard &>/dev/null; then
    log_info "Starting FOC sidecar..."
    /opt/antithesis/foc-sidecar &
    SIDECAR_PID=$!
    log_info "FOC sidecar started (PID=$SIDECAR_PID)"
fi

# ── 8. Launch stress engine ──
log_info "Launching stress engine..."
/opt/antithesis/stress-engine &
STRESS_PID=$!

if [ "${FUZZER_ENABLED:-1}" = "1" ]; then
    log_info "Launching protocol fuzzer..."
    /opt/antithesis/protocol-fuzzer &
    FUZZER_PID=$!
fi

wait -n $STRESS_PID ${FUZZER_PID:-}
