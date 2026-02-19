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
WAIT_HEIGHT="${ENTRYPOINT_WAIT_HEIGHT:-5}"
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

# ── 4. Wait for filwizard to finish (skip if FILWIZARD_ENABLED != true) ──
ENV_FILE="/shared/environment.env"
if [ "${FILWIZARD_ENABLED:-false}" = "true" ]; then
    log_info "Waiting for environment.env from filwizard..."
    while [ ! -f "$ENV_FILE" ] || [ ! -s "$ENV_FILE" ]; do sleep 2; done
    log_info "environment.env ready"

    # Source it (for any scripts that need vars)
    set -a
    source "$ENV_FILE"
    set +a
else
    log_info "Skipping filwizard wait (FILWIZARD_ENABLED=false)"
fi

# ── 5. Signal setup complete to Antithesis ──
log_info "Signaling setup complete..."
if [ -f "/opt/antithesis/entrypoint/setup_complete.py" ]; then
    python3 -u /opt/antithesis/entrypoint/setup_complete.py
fi

# ── 6. Launch stress engine ──
log_info "Launching stress engine..."
exec /opt/antithesis/stress-engine
