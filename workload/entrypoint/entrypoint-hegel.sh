#!/bin/bash
set -e

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log_info() { echo -e "${GREEN}[HEGEL]${NC} $1"; }
log_warn() { echo -e "${YELLOW}[HEGEL]${NC} $1"; }

# ── 1. Ensure Antithesis output directory exists ──
mkdir -p "${ANTITHESIS_OUTPUT_DIR:-/tmp/antithesis}"

# ── 2. Wait for genesis keystore (produced by the main workload container) ──
KEYSTORE="${STRESS_KEYSTORE_PATH:-/shared/configs/stress_keystore.json}"
log_info "Waiting for keystore at ${KEYSTORE}..."
while [ ! -f "$KEYSTORE" ] || [ ! -s "$KEYSTORE" ]; do sleep 2; done
log_info "Keystore ready."

# ── 3. Wait for node multiaddr files ──
DEVGEN_DIR="${DEVGEN_DIR:-/root/devgen}"
IFS=',' read -ra NODES <<< "${STRESS_NODES:-lotus0}"
log_info "Waiting for node multiaddr files..."
MAX_WAIT=300
WAITED=0
FOUND=false
while [ "$WAITED" -lt "$MAX_WAIT" ]; do
    for node in "${NODES[@]}"; do
        node=$(echo "$node" | tr -d ' ')
        ADDR_FILE="${DEVGEN_DIR}/${node}/${node}-ipv4addr"
        if [ -f "$ADDR_FILE" ] && [ -s "$ADDR_FILE" ]; then
            log_info "Found multiaddr for ${node}"
            FOUND=true
            break
        fi
    done
    if [ "$FOUND" = true ]; then
        break
    fi
    sleep 5
    WAITED=$((WAITED + 5))
done
if [ "$FOUND" = false ]; then
    log_warn "No multiaddr files found after ${MAX_WAIT}s, continuing anyway..."
fi

# ── 4. Wait for blockchain to reach minimum epoch ──
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

# ── 5. Launch hegel workload ──
log_info "Launching hegel workload..."
exec env RUST_LOG="${RUST_LOG:-info}" /usr/local/bin/hegel-workload
