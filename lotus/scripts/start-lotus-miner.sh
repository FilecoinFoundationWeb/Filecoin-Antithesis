#!/bin/bash

node_number="$1"

lotus_miner_actor_address="LOTUS_MINER_${node_number}_ACTOR_ADDRESS"
export LOTUS_MINER_ACTOR_ADDRESS="${!lotus_miner_actor_address}"

lotus_path="LOTUS_${node_number}_PATH"
export LOTUS_PATH="${!lotus_path}"

lotus_miner_path="LOTUS_MINER_${node_number}_PATH"
export LOTUS_MINER_PATH="${!lotus_miner_path}"

export LOTUS_F3_BOOTSTRAP_EPOCH=21
export DRAND_CHAIN_INFO=chain_info
export LOTUS_SKIP_GENESIS_CHECK=${LOTUS_SKIP_GENESIS_CHECK}
export CGO_CFLAGS_ALLOW="-D__BLST_PORTABLE__"
export CGO_CFLAGS="-D__BLST_PORTABLE__"

lotus-miner --version
lotus wallet import --as-default "${SHARED_CONFIGS}/.genesis-sector-${node_number}/pre-seal-${LOTUS_MINER_ACTOR_ADDRESS}.key"

if [ -f "${LOTUS_MINER_PATH}/config.toml" ]; then
    echo "lotus-miner${node_number}: Repo already exists, skipping init..."
else
    if [ "$node_number" -eq 0 ]; then
        lotus-miner init --genesis-miner --actor=${LOTUS_MINER_ACTOR_ADDRESS} --sector-size=2KiB --pre-sealed-sectors=${SHARED_CONFIGS}/.genesis-sector-${node_number} --pre-sealed-metadata=${SHARED_CONFIGS}/manifest.json --nosync
    else
        lotus-miner init --actor=${LOTUS_MINER_ACTOR_ADDRESS} --sector-size=2KiB --pre-sealed-sectors=${SHARED_CONFIGS}/.genesis-sector-${node_number} --pre-sealed-metadata=${SHARED_CONFIGS}/manifest.json --nosync
    fi
fi
echo "lotus-miner${node_number}: setup complete"

# Reduce log noise: set all subsystems to error first, then selectively raise to warn.
# The full list is generated from `lotus-miner log list`.
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

lotus-miner log set-level "${SYSTEM_FLAGS[@]}" error
lotus-miner log set-level "${SYSTEM_FLAGS[@]}" warn
lotus-miner run --nosync
