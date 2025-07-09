#!/bin/bash

sleep 5

# Common environment setup
export LOTUS_F3_BOOTSTRAP_EPOCH=21
export LOTUS_PATH=${LOTUS_1_PATH}
export LOTUS_MINER_PATH=${LOTUS_MINER_1_PATH}
export LOTUS_SKIP_GENESIS_CHECK=${LOTUS_SKIP_GENESIS_CHECK}
export CGO_CFLAGS_ALLOW="-D__BLST_PORTABLE__"
export CGO_CFLAGS="-D__BLST_PORTABLE__"
export LOTUS_CHAININDEXER_ENABLEINDEXER=true

# Check if initialization is needed by looking for key files
if [ ! -f "${LOTUS_1_DATA_DIR}/key" ] || [ ! -f "${LOTUS_1_DATA_DIR}/config.toml" ] || [ ! -f "${LOTUS_1_DATA_DIR}/devgen.car" ]; then
    INIT_MODE=true
    echo "lotus-1: First run detected, performing initialization..."
else
    INIT_MODE=false
    echo "lotus-1: Found existing setup, running in daemon-only mode..."
fi

# Always get fresh chain info
if [ "$INIT_MODE" = "true" ]; then
    tries=10
    while [ "$tries" -gt 0 ]; do
        curl 10.20.20.21/info | jq -c
        chain_info_status=$?
        if [ $chain_info_status -eq 0 ];
        then
            echo "lotus-1: chain_info is ready!"
            echo "lotus-1: continuing startup..."
            break
        fi
        sleep 3
        tries=$(( tries - 1 ))
        echo "lotus-1: $tries connection attempts remaining..."
    done
fi

curl 10.20.20.21/info | jq -c > chain_info
export DRAND_CHAIN_INFO=chain_info

lotus --version

# Function to connect to peers with retries
connect_to_peers() {
    max_attempts=5
    for peer in "${LOTUS_2_DATA_DIR}/ipv4addr" "${FOREST_DATA_DIR}/forest-listen-addr"; do
        if [ -f "$peer" ]; then
            attempt=1
            while [ $attempt -le $max_attempts ]; do
                echo "lotus-1: Attempting to connect to peer from $peer (attempt $attempt/$max_attempts)"
                if lotus net connect $(cat $peer); then
                    echo "lotus-1: Successfully connected to peer from $peer"
                    break
                fi
                echo "lotus-1: Failed to connect to peer from $peer"
                attempt=$((attempt + 1))
                sleep 5
            done
        else
            echo "lotus-1: Peer address file $peer not found"
        fi
    done
}

# Initialization steps
if [ "$INIT_MODE" = "true" ]; then
    echo "lotus-1: Running in initialization mode..."
    cp /root/.genesis-sector-1/pre-seal-t01000.key ${LOTUS_1_DATA_DIR}/key
    cp /lotus_instrumented/customer/config-1.toml "${LOTUS_1_DATA_DIR}/config.toml"
    cat localnet-1.json | jq -r '.NetworkName' > ${LOTUS_1_DATA_DIR}/network_name
    cp localnet-1.json ${LOTUS_1_DATA_DIR}/localnet.json
    
    # Set log levels for initialization
   # lotus log set-level --system panic-reporter --system incrt --system bitswap-client --system table --system pubsub --system test-logger --system routedhost --system f3/internal/caching --system engine --system badgerbs --system chainstore --system genesis --system messagesigner --system sqlite --system providers --system miner --system f3/certexchange --system cliutil --system lotus-tracer --system fullnode --system gen --system tarutil --system ipns --system websocket-transport --system cli --system stores --system paramfetch --system amt --system splitstore --system blockservice --system webrtc-transport-pion --system build --system ctladdr --system pstoremanager --system quic-utils --system repo --system wallet-ledger --system lock --system dht.pb --system blankhost --system mocknet --system chainindex --system hello --system httpreader --system build/buildtypes --system tracing --system advmgr --system webrtc-udpmux --system paych --system healthcheck --system beacon --system statetree --system bundle --system connmgr --system swarm2 --system chainxchg --system chain --system harmonydb --system peerstore --system net/identify --system autonatv2 --system relay --system fsjournal --system peermgr --system builder --system alerting --system webtransport --system bs:peermgr --system merkledag --system discovery-backoff --system basichost --system disputer --system storageminer --system backupds --system rpcenc --system pathresolv --system peerstore/ds --system sub --system bs:sess --system consensus-common --system f3/manifest-provider --system wallet --system api_proxy --system wdpost --system eventlog --system types --system autonat --system p2p-circuit --system bitswap-server --system actors --system modules --system bitswap --system ulimit --system pubsub/timecache --system slashsvc --system quic-transport --system p2pnode --system payment-channel-settler --system partialfile --system cborrrpc --system nat --system sectors --system canonical-log --system f3/ohshitstore --system diversityFilter --system f3 --system ffiwrapper --system main --system f3/gpbft --system bs:sprmgr --system dht/RtRefreshManager --system drand --system f3/wal --system blockstore --system routing/composable --system bitswap_network --system rand --system market_adapter --system fsutil --system evtsm --system autorelay --system preseal --system node --system system --system p2p-holepunch --system messagepool --system watchdog --system metrics-prometheus --system ping --system reuseport-transport --system resources --system dht/netsize --system fil-consensus --system metrics --system events --system p2p-config --system dht --system net/conngater --system vm --system auth --system webrtc-transport --system badger --system rcmgr --system tcp-tpt --system retry --system upgrader --system statemgr --system conngater --system f3/blssig --system rpc warn
   # lotus log set-level --system panic-reporter --system incrt --system bitswap-client --system table --system pubsub --system test-logger --system routedhost --system f3/internal/caching --system engine --system badgerbs --system chainstore --system genesis --system messagesigner --system sqlite --system providers --system miner --system f3/certexchange --system cliutil --system lotus-tracer --system fullnode --system gen --system tarutil --system ipns --system websocket-transport --system cli --system stores --system paramfetch --system amt --system splitstore --system blockservice --system webrtc-transport-pion --system build --system ctladdr --system pstoremanager --system quic-utils --system repo --system wallet-ledger --system lock --system dht.pb --system blankhost --system mocknet --system chainindex --system hello --system httpreader --system build/buildtypes --system tracing --system advmgr --system webrtc-udpmux --system paych --system healthcheck --system beacon --system statetree --system bundle --system connmgr --system swarm2 --system chainxchg --system chain --system harmonydb --system peerstore --system net/identify --system autonatv2 --system relay --system fsjournal --system peermgr --system builder --system alerting --system webtransport --system bs:peermgr --system merkledag --system discovery-backoff --system basichost --system disputer --system storageminer --system backupds --system rpcenc --system pathresolv --system peerstore/ds --system sub --system bs:sess --system consensus-common --system f3/manifest-provider --system wallet --system api_proxy --system wdpost --system eventlog --system types --system autonat --system p2p-circuit --system bitswap-server --system actors --system modules --system bitswap --system ulimit --system pubsub/timecache --system slashsvc --system quic-transport --system p2pnode --system payment-channel-settler --system partialfile --system cborrrpc --system nat --system sectors --system canonical-log --system f3/ohshitstore --system diversityFilter --system f3 --system ffiwrapper --system main --system f3/gpbft --system bs:sprmgr --system dht/RtRefreshManager --system drand --system f3/wal --system blockstore --system routing/composable --system bitswap_network --system rand --system market_adapter --system fsutil --system evtsm --system autorelay --system preseal --system node --system system --system p2p-holepunch --system messagepool --system watchdog --system metrics-prometheus --system ping --system reuseport-transport --system resources --system dht/netsize --system fil-consensus --system metrics --system events --system p2p-config --system dht --system net/conngater --system vm --system auth --system webrtc-transport --system badger --system rcmgr --system tcp-tpt --system retry --system upgrader --system statemgr --system conngater --system f3/blssig --system rpc error
    
    # Start daemon with genesis creation
    lotus daemon --lotus-make-genesis=${LOTUS_1_DATA_DIR}/devgen.car --genesis-template=${LOTUS_1_DATA_DIR}/localnet.json --bootstrap=false --config=${LOTUS_1_DATA_DIR}/config.toml&
else
    echo "lotus-1: Running in daemon-only mode..."
    # Start daemon without genesis creation
    lotus daemon --bootstrap=false --config=${LOTUS_1_DATA_DIR}/config.toml&
fi

# Common post-startup steps
lotus wait-api
echo "lotus-1: finished waiting for API, proceeding with network setup."

lotus net listen > ${LOTUS_1_DATA_DIR}/ipv4addr
cat ${LOTUS_1_DATA_DIR}/ipv4addr | awk 'NR==1 {print; exit}' > ${LOTUS_1_DATA_DIR}/lotus-1-ipv4addr
lotus net id > ${LOTUS_1_DATA_DIR}/p2pID
lotus auth create-token --perm admin > ${LOTUS_1_DATA_DIR}/jwt

# Connect to peers with retries
connect_to_peers

sleep infinity
