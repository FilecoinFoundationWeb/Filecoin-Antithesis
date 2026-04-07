#!/bin/bash
set -e

DRAND_DIR="/root/.drand"
DKG_COMPLETE_MARKER="${DRAND_DIR}/multibeacon/default/groups/drand_group.toml"

# ---------------------------------------------------------------------------
# Restart path: DKG already completed, just resume beacon production
# ---------------------------------------------------------------------------
if [ -f "$DKG_COMPLETE_MARKER" ]; then
    echo "drand2: DKG already complete — restarting daemon in foreground"
    exec drand start --private-listen drand2:8080 --control 127.0.0.1:8888 --public-listen 0.0.0.0:80
fi

# ---------------------------------------------------------------------------
# Fresh start: generate keypair, join DKG ceremony, then wait on daemon
# ---------------------------------------------------------------------------
echo "drand2: fresh start — generating keypair and joining DKG"

drand generate-keypair --scheme bls-unchained-g1-rfc9380 --id default drand2:8080

# Start daemon in background so we can run DKG commands against it
drand start --private-listen drand2:8080 --control 127.0.0.1:8888 --public-listen 0.0.0.0:80 &
DRAND_PID=$!

# Wait for local daemon to be ready
echo "drand2: waiting for local daemon to start..."
tries=15
while [ "$tries" -gt 0 ]; do
    drand util check drand2:8080 2>/dev/null && break
    sleep 1
    tries=$(( tries - 1 ))
done
if [ "$tries" -eq 0 ]; then
    echo "ERROR: local drand2 daemon never became ready"
    exit 1
fi
echo "drand2: local daemon ready — waiting for DKG proposal from leader"

# Wait for DKG proposal to be available
tries=30
while [ "$tries" -gt 0 ]; do
    echo "drand2: checking dkg status..."
    lines=$(drand dkg status --control 8888 2>/dev/null | wc -l)
    if [ "$lines" -gt 10 ]; then
        echo "drand2: dkg status up"
        break
    fi
    tries=$(( tries - 1 ))
    echo "drand2: $tries attempts remaining..."
    sleep 1
done

if [ "$tries" -eq 0 ]; then
    echo "ERROR: drand2 DKG status never ready"
    exit 1
fi

# Join the DKG process initiated by the leader
drand dkg join --control 8888

# Keep container alive by waiting on the daemon process
wait $DRAND_PID
