#!/bin/bash
set -euo pipefail

echo "[hegel-workload] starting entrypoint"

# Ensure Antithesis output directory exists (needed by hegeltest SDK even outside Antithesis)
mkdir -p "${ANTITHESIS_OUTPUT_DIR:-/tmp/antithesis}"

# Default devgen directory
DEVGEN_DIR="${DEVGEN_DIR:-/root/devgen}"

# Parse STRESS_NODES into an array
IFS=',' read -ra NODES <<< "${STRESS_NODES:-lotus0}"

# Wait for at least one node's multiaddr file to exist
echo "[hegel-workload] waiting for node multiaddr files..."
MAX_WAIT=300
WAITED=0
FOUND=false
while [ "$WAITED" -lt "$MAX_WAIT" ]; do
    for node in "${NODES[@]}"; do
        node=$(echo "$node" | tr -d ' ')
        ADDR_FILE="${DEVGEN_DIR}/${node}/${node}-ipv4addr"
        if [ -f "$ADDR_FILE" ] && [ -s "$ADDR_FILE" ]; then
            echo "[hegel-workload] found multiaddr for ${node}"
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
    echo "[hegel-workload] ERROR: no multiaddr files found after ${MAX_WAIT}s"
    exit 1
fi

# Wait for network_name file
NETWORK_FILE="${DEVGEN_DIR}/lotus0/network_name"
echo "[hegel-workload] waiting for network name at ${NETWORK_FILE}..."
WAITED=0
while [ "$WAITED" -lt "$MAX_WAIT" ]; do
    if [ -f "$NETWORK_FILE" ] && [ -s "$NETWORK_FILE" ]; then
        echo "[hegel-workload] network name: $(cat "$NETWORK_FILE")"
        break
    fi
    sleep 5
    WAITED=$((WAITED + 5))
done

# Give nodes a few more seconds to finish connecting to each other
sleep 10

echo "[hegel-workload] launching hegel-workload binary"
exec /usr/local/bin/hegel-workload
