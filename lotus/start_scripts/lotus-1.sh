#!/bin/bash

# Function to wait for chain_info
wait_for_chain_info() {
    local tries=10
    while [ "$tries" -gt 0 ]; do
        if response=$(curl -s 10.20.20.21/info) && echo "$response" | jq . >/dev/null 2>&1; then
            echo "lotus-1: chain_info is ready!"
            return 0
        fi
        echo "lotus-1: $tries connection attempts remaining..."
        sleep 3
        tries=$((tries - 1))
    done
    return 1
}

# Initialize environment
export LOTUS_F3_BOOTSTRAP_EPOCH=21
export LOTUS_PATH=${LOTUS_1_PATH}
export LOTUS_MINER_PATH=${LOTUS_MINER_1_PATH}
export LOTUS_SKIP_GENESIS_CHECK=${LOTUS_SKIP_GENESIS_CHECK}
export CGO_CFLAGS_ALLOW="-D__BLST_PORTABLE__"
export CGO_CFLAGS="-D__BLST_PORTABLE__"
export LOTUS_CHAININDEXER_ENABLEINDEXER=true

# Wait for chain info and setup
wait_for_chain_info

# Store chain info with proper error handling
if ! curl -s 10.20.20.21/info > chain_info.tmp || ! jq . chain_info.tmp > chain_info 2>/dev/null; then
    echo "lotus-1: Failed to get valid chain info"
    exit 1
fi
export DRAND_CHAIN_INFO=chain_info

# Print version and setup config
lotus --version
cp /root/.genesis-sector-1/pre-seal-t01000.key ${LOTUS_1_DATA_DIR}/key
cp /lotus_instrumented/customer/config-1.toml "${LOTUS_1_DATA_DIR}/config.toml"
cat localnet-1.json | jq -r '.NetworkName' > ${LOTUS_1_DATA_DIR}/network_name
cp localnet-1.json ${LOTUS_1_DATA_DIR}/localnet.json

# Set log levels
lotus log set-level --system panic-reporter --system incrt --system bitswap-client --system table --system pubsub --system test-logger --system routedhost --system f3/internal/caching --system engine --system badgerbs --system chainstore --system genesis --system messagesigner --system sqlite --system providers --system miner --system f3/certexchange --system cliutil --system lotus-tracer --system fullnode --system gen --system tarutil --system ipns --system websocket-transport --system cli --system stores --system paramfetch --system amt --system splitstore --system blockservice --system webrtc-transport-pion --system build --system ctladdr --system pstoremanager --system quic-utils --system repo --system wallet-ledger --system lock --system dht.pb --system blankhost --system mocknet --system chainindex --system hello --system httpreader --system build/buildtypes --system tracing --system advmgr --system webrtc-udpmux --system paych --system healthcheck --system beacon --system statetree --system bundle --system connmgr --system swarm2 --system chainxchg --system chain --system harmonydb --system peerstore --system net/identify --system autonatv2 --system relay --system fsjournal --system peermgr --system builder --system alerting --system webtransport --system bs:peermgr --system merkledag --system discovery-backoff --system basichost --system disputer --system storageminer --system backupds --system rpcenc --system pathresolv --system peerstore/ds --system sub --system bs:sess --system consensus-common --system f3/manifest-provider --system wallet --system api_proxy --system wdpost --system eventlog --system types --system autonat --system p2p-circuit --system bitswap-server --system actors --system modules --system bitswap --system ulimit --system pubsub/timecache --system slashsvc --system quic-transport --system p2pnode --system payment-channel-settler --system partialfile --system cborrrpc --system nat --system sectors --system canonical-log --system f3/ohshitstore --system diversityFilter --system f3 --system ffiwrapper --system main --system f3/gpbft --system bs:sprmgr --system dht/RtRefreshManager --system drand --system f3/wal --system blockstore --system routing/composable --system bitswap_network --system rand --system market_adapter --system fsutil --system evtsm --system autorelay --system preseal --system node --system system --system p2p-holepunch --system messagepool --system watchdog --system metrics-prometheus --system ping --system reuseport-transport --system resources --system dht/netsize --system fil-consensus --system metrics --system events --system p2p-config --system dht --system net/conngater --system vm --system auth --system webrtc-transport --system badger --system rcmgr --system tcp-tpt --system retry --system upgrader --system statemgr --system conngater --system f3/blssig --system rpc warn
lotus log set-level --system panic-reporter --system incrt --system bitswap-client --system table --system pubsub --system test-logger --system routedhost --system f3/internal/caching --system engine --system badgerbs --system chainstore --system genesis --system messagesigner --system sqlite --system providers --system miner --system f3/certexchange --system cliutil --system lotus-tracer --system fullnode --system gen --system tarutil --system ipns --system websocket-transport --system cli --system stores --system paramfetch --system amt --system splitstore --system blockservice --system webrtc-transport-pion --system build --system ctladdr --system pstoremanager --system quic-utils --system repo --system wallet-ledger --system lock --system dht.pb --system blankhost --system mocknet --system chainindex --system hello --system httpreader --system build/buildtypes --system tracing --system advmgr --system webrtc-udpmux --system paych --system healthcheck --system beacon --system statetree --system bundle --system connmgr --system swarm2 --system chainxchg --system chain --system harmonydb --system peerstore --system net/identify --system autonatv2 --system relay --system fsjournal --system peermgr --system builder --system alerting --system webtransport --system bs:peermgr --system merkledag --system discovery-backoff --system basichost --system disputer --system storageminer --system backupds --system rpcenc --system pathresolv --system peerstore/ds --system sub --system bs:sess --system consensus-common --system f3/manifest-provider --system wallet --system api_proxy --system wdpost --system eventlog --system types --system autonat --system p2p-circuit --system bitswap-server --system actors --system modules --system bitswap --system ulimit --system pubsub/timecache --system slashsvc --system quic-transport --system p2pnode --system payment-channel-settler --system partialfile --system cborrrpc --system nat --system sectors --system canonical-log --system f3/ohshitstore --system diversityFilter --system f3 --system ffiwrapper --system main --system f3/gpbft --system bs:sprmgr --system dht/RtRefreshManager --system drand --system f3/wal --system blockstore --system routing/composable --system bitswap_network --system rand --system market_adapter --system fsutil --system evtsm --system autorelay --system preseal --system node --system system --system p2p-holepunch --system messagepool --system watchdog --system metrics-prometheus --system ping --system reuseport-transport --system resources --system dht/netsize --system fil-consensus --system metrics --system events --system p2p-config --system dht --system net/conngater --system vm --system auth --system webrtc-transport --system badger --system rcmgr --system tcp-tpt --system retry --system upgrader --system statemgr --system conngater --system f3/blssig --system rpc error

# Start daemon and wait for it to be ready
lotus daemon --lotus-make-genesis=${LOTUS_1_DATA_DIR}/devgen.car --genesis-template=${LOTUS_1_DATA_DIR}/localnet.json --bootstrap=false --config=${LOTUS_1_DATA_DIR}/config.toml &

# Wait for API and verify node is ready
lotus wait-api
echo "lotus-1: waiting for node to be ready..."
max_attempts=30
attempt=1
while [ $attempt -le $max_attempts ]; do
    if lotus chain head >/dev/null 2>&1; then
        echo "lotus-1: node is ready"
        break
    fi
    echo "lotus-1: waiting for node (attempt ${attempt}/${max_attempts})"
    sleep 2
    attempt=$((attempt + 1))
done

# Setup network info and create readiness flag
lotus net listen > ${LOTUS_1_DATA_DIR}/ipv4addr
cat ${LOTUS_1_DATA_DIR}/ipv4addr | awk 'NR==1 {print; exit}' > ${LOTUS_1_DATA_DIR}/lotus-1-ipv4addr
lotus net id > ${LOTUS_1_DATA_DIR}/p2pID
lotus auth create-token --perm admin > ${LOTUS_1_DATA_DIR}/jwt

# Signal that node is ready for connections
touch ${LOTUS_1_DATA_DIR}/.node_ready

# Cleanup temporary files
rm -f chain_info.tmp

echo "lotus-1: ready for connections"
sleep infinity
