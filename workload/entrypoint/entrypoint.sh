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

# ── 3. Wait for filwizard to finish ──
ENV_FILE="/shared/environment.env"
log_info "Waiting for environment.env from filwizard..."
while [ ! -f "$ENV_FILE" ] || [ ! -s "$ENV_FILE" ]; do sleep 2; done
log_info "environment.env ready"

# Source it (for any scripts that need vars)
set -a
source "$ENV_FILE"
set +a

# ── 4. Signal setup complete to Antithesis ──
log_info "Signaling setup complete..."
if [ -f "/opt/antithesis/entrypoint/setup_complete.py" ]; then
    python3 -u /opt/antithesis/entrypoint/setup_complete.py
fi

# ── 5. Launch stress engine ──
log_info "Launching stress engine..."
exec /opt/antithesis/stress-engine
