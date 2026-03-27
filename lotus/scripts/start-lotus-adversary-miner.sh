#!/bin/bash

no="$1"

# Connect to the adversary full node (not a regular lotus node)
lotus_adversary_path="LOTUS_ADVERSARY_${no}_PATH"
export LOTUS_PATH="${!lotus_adversary_path}"

lotus_adversary_miner_actor="LOTUS_ADVERSARY_MINER_${no}_ACTOR_ADDRESS"
export LOTUS_MINER_ACTOR_ADDRESS="${!lotus_adversary_miner_actor}"

lotus_adversary_miner_path="LOTUS_ADVERSARY_MINER_${no}_PATH"
export LOTUS_MINER_PATH="${!lotus_adversary_miner_path}"

export LOTUS_F3_BOOTSTRAP_EPOCH=21
export DRAND_CHAIN_INFO=chain_info
export LOTUS_SKIP_GENESIS_CHECK=${LOTUS_SKIP_GENESIS_CHECK}
export CGO_CFLAGS_ALLOW="-D__BLST_PORTABLE__"
export CGO_CFLAGS="-D__BLST_PORTABLE__"

lotus-miner --version
lotus wallet import --as-default "${SHARED_CONFIGS}/.genesis-sector-adversary-${no}/pre-seal-${LOTUS_MINER_ACTOR_ADDRESS}.key"

if [ -f "${LOTUS_MINER_PATH}/config.toml" ]; then
    echo "lotus-adversary-miner${no}: Repo already exists, skipping init..."
else
    lotus-miner init --actor=${LOTUS_MINER_ACTOR_ADDRESS} --sector-size=2KiB \
        --pre-sealed-sectors=${SHARED_CONFIGS}/.genesis-sector-adversary-${no} \
        --pre-sealed-metadata=${SHARED_CONFIGS}/manifest.json --nosync

    if [ $? -ne 0 ]; then
        echo "ERROR: lotus-adversary-miner${no} init failed, exiting"
        exit 1
    fi
fi
echo "lotus-adversary-miner${no}: setup complete"

# Start miner in background, then configure log levels once API is up
lotus-miner run --nosync &
MINER_PID=$!

# Wait for miner API to become available before setting log levels
echo "lotus-adversary-miner${no}: waiting for miner API..."
for i in $(seq 1 30); do
    if lotus-miner auth api-info --perm admin >/dev/null 2>&1; then
        break
    fi
    sleep 1
done

# Reduce log noise: set all subsystems to warn, then suppress F3 polling
LOG_SYSTEMS=(
    panic-reporter incrt bitswap-client table pubsub test-logger routedhost
    f3/internal/caching engine badgerbs chainstore genesis messagesigner sqlite
    providers miner f3/certexchange cliutil lotus-tracer fullnode gen tarutil
    ipns websocket-transport cli stores paramfetch amt splitstore blockservice
    webrtc-transport-pion build ctladdr pstoremanager quic-utils repo
    wallet-ledger lock dht.pb blankhost mocknet chainindex hello httpreader
    build/buildtypes tracing advmgr webrtc-udpmux paych healthcheck beacon
    statetree bundle connmgr swarm2 chainxchg chain harmonydb peerstore
    net/identify autonatv2 relay fsjournal peermgr builder alerting webtransport
    bs:peermgr merkledag discovery-backoff basichost disputer storageminer
    backupds rpcenc pathresolv peerstore/ds sub bs:sess consensus-common
    f3/manifest-provider wallet api_proxy wdpost eventlog types autonat
    p2p-circuit bitswap-server actors modules bitswap ulimit pubsub/timecache
    slashsvc quic-transport p2pnode payment-channel-settler partialfile cborrrpc
    nat sectors canonical-log f3/ohshitstore diversityFilter f3 ffiwrapper main
    f3/gpbft bs:sprmgr dht/RtRefreshManager drand f3/wal blockstore
    routing/composable bitswap_network rand market_adapter fsutil evtsm
    autorelay preseal node system p2p-holepunch messagepool watchdog
    metrics-prometheus ping reuseport-transport resources dht/netsize
    fil-consensus metrics events p2p-config dht net/conngater vm auth
    webrtc-transport badger rcmgr tcp-tpt retry upgrader statemgr conngater
    f3/blssig rpc
)

SYSTEM_FLAGS=()
for sys in "${LOG_SYSTEMS[@]}"; do
    SYSTEM_FLAGS+=("--system" "$sys")
done

lotus-miner log set-level "${SYSTEM_FLAGS[@]}" warn 2>/dev/null || true

# Suppress noisy F3 bootstrap polling ("waiting for bootstrap epoch" every ~20ms)
F3_SYSTEMS=(f3 f3/internal/caching f3/certexchange f3/manifest-provider f3/ohshitstore f3/gpbft f3/wal f3/blssig)
F3_FLAGS=()
for sys in "${F3_SYSTEMS[@]}"; do
    F3_FLAGS+=("--system" "$sys")
done
lotus-miner log set-level "${F3_FLAGS[@]}" error 2>/dev/null || true

wait $MINER_PID
