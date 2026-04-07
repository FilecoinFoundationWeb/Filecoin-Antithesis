#!/bin/bash
set -e

DRAND_DIR="/root/.drand"
DKG_COMPLETE_MARKER="${DRAND_DIR}/multibeacon/default/groups/drand_group.toml"

# ---------------------------------------------------------------------------
# Restart path: DKG already completed, just resume beacon production
# ---------------------------------------------------------------------------
if [ -f "$DKG_COMPLETE_MARKER" ]; then
    echo "drand0: DKG already complete — restarting daemon in foreground"
    exec drand start --id default --private-listen drand0:8080 --control 127.0.0.1:8888 --public-listen 0.0.0.0:80
fi

# ---------------------------------------------------------------------------
# Fresh start: generate keypair, run DKG ceremony, then wait on daemon
# ---------------------------------------------------------------------------
echo "drand0: fresh start — generating keypair and initializing DKG"

drand generate-keypair --scheme bls-unchained-g1-rfc9380 --id default drand0:8080

# Start daemon in background so we can run DKG commands against it
drand start --id default --private-listen drand0:8080 --control 127.0.0.1:8888 --public-listen 0.0.0.0:80 &
DRAND_PID=$!

# Wait for local daemon to be ready before checking peers
echo "drand0: waiting for local daemon to start..."
tries=15
while [ "$tries" -gt 0 ]; do
    if drand util check drand0:8080 2>/dev/null; then break; fi
    sleep 1
    tries=$(( tries - 1 ))
done
if [ "$tries" -eq 0 ]; then
    echo "ERROR: local drand0 daemon never became ready"
    exit 1
fi
echo "drand0: local daemon ready"

# Wait until drand1 and drand2 are up
tries=30
while [ "$tries" -gt 0 ]; do
    drand1_status=0
    drand2_status=0
    drand util check drand1:8080 2>/dev/null || drand1_status=$?
    drand util check drand2:8080 2>/dev/null || drand2_status=$?
    if [ $drand1_status -eq 0 ] && [ $drand2_status -eq 0 ]; then
        echo "drand0: discovered drand1 and drand2"
        break
    fi
    sleep 1
    tries=$(( tries - 1 ))
    echo "drand0: $tries connection attempts remaining..."
done

if [ "$tries" -eq 0 ]; then
    echo "ERROR: timed out waiting for drand1 and drand2"
    exit 1
fi

echo "drand0: initializing DKG as leader..."
drand dkg generate-proposal --joiner drand0:8080 --joiner drand1:8080 --joiner drand2:8080 --out proposal.toml

drand dkg init --proposal proposal.toml --threshold 2 --period 3s --scheme bls-unchained-g1-rfc9380 --catchup-period 0s --genesis-delay 30s

drand dkg execute

# Keep container alive by waiting on the daemon process
wait $DRAND_PID
