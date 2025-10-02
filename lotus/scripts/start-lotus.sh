#!/bin/bash

no="$1"

lotus_data_dir="LOTUS_${no}_DATA_DIR"
export LOTUS_DATA_DIR="${!lotus_data_dir}"

lotus_ip="LOTUS_${no}_IP"
export LOTUS_IP="${!lotus_ip}"

lotus_rpc_port="LOTUS_${no}_RPC_PORT"
export LOTUS_RPC_PORT="${!lotus_rpc_port}"

export LOTUS_F3_BOOTSTRAP_EPOCH=21
export LOTUS_CHAININDEXER_ENABLEINDEXER=true
export DRAND0_IP=${DRAND0_IP}

# required via docs:
lotus_path="LOTUS_${no}_PATH"
export LOTUS_PATH="${!lotus_path}"

lotus_miner_path="LOTUS_MINER_${no}_PATH"
export LOTUS_MINER_PATH="${!lotus_miner_path}"

export LOTUS_SKIP_GENESIS_CHECK=${LOTUS_SKIP_GENESIS_CHECK}
export CGO_CFLAGS_ALLOW="-D__BLST_PORTABLE__"
export CGO_CFLAGS="-D__BLST_PORTABLE__"


# check if initialization is needed
if [ ! -f "${LOTUS_DATA_DIR}/config.toml" ]; then
    INIT_MODE=true
    echo "lotus${no}: First run detected, performing initialization..."
else
    INIT_MODE=false
    echo "lotus${no}: Found existing setup, running in daemon-only mode..."
fi


# get drand info
echo "polling drand0 (${DRAND_IP}) until a valid JSON response is received..."

OUTPUT_FILE="chain_info.json"
while true; do
    # -s suppresses progress bar from curl and --fail causes curl to fail on HTTP errors
    response=$(curl -s --fail "http://${DRAND0_IP}/info")

    if echo "$response" | jq -e '.public_key?' >/dev/null 2>&1; then
        echo "$response" > "$OUTPUT_FILE"
        export DRAND_CHAIN_INFO=$(jq -c . "$OUTPUT_FILE")
        break
    else
        echo "No valid response yet... retrying in 2 seconds."
        sleep 2
    fi
done

echo "Compact drand chain info:"
echo "$DRAND_CHAIN_INFO"

# initialization steps
if [ "$INIT_MODE" = "true" ]; then
    echo "lotus${no}: Running in initialization mode..."
    
    # creating config.toml based on the template and the lotus number
    sed "s/\${LOTUS_IP}/$LOTUS_IP/g; s/\${LOTUS_RPC_PORT}/$LOTUS_RPC_PORT/g" config.toml.template > config.toml

    # initialize the miners    
    if [ "$no" -eq 0 ]; then
        ./scripts/setup-genesis.sh
    fi

    # saving the network name
    cat ${SHARED_CONFIGS}/localnet.json | jq -r '.NetworkName' > ${LOTUS_DATA_DIR}/network_name
    
    # set log levels for initialization
    lotus log set-level --system panic-reporter --system incrt --system bitswap-client --system table --system pubsub --system test-logger --system routedhost --system f3/internal/caching --system engine --system badgerbs --system chainstore --system genesis --system messagesigner --system sqlite --system providers --system miner --system f3/certexchange --system cliutil --system lotus-tracer --system fullnode --system gen --system tarutil --system ipns --system websocket-transport --system cli --system stores --system paramfetch --system amt --system splitstore --system blockservice --system webrtc-transport-pion --system build --system ctladdr --system pstoremanager --system quic-utils --system repo --system wallet-ledger --system lock --system dht.pb --system blankhost --system mocknet --system chainindex --system hello --system httpreader --system build/buildtypes --system tracing --system advmgr --system webrtc-udpmux --system paych --system healthcheck --system beacon --system statetree --system bundle --system connmgr --system swarm2 --system chainxchg --system chain --system harmonydb --system peerstore --system net/identify --system autonatv2 --system relay --system fsjournal --system peermgr --system builder --system alerting --system webtransport --system bs:peermgr --system merkledag --system discovery-backoff --system basichost --system disputer --system storageminer --system backupds --system rpcenc --system pathresolv --system peerstore/ds --system sub --system bs:sess --system consensus-common --system f3/manifest-provider --system wallet --system api_proxy --system wdpost --system eventlog --system types --system autonat --system p2p-circuit --system bitswap-server --system actors --system modules --system bitswap --system ulimit --system pubsub/timecache --system slashsvc --system quic-transport --system p2pnode --system payment-channel-settler --system partialfile --system cborrrpc --system nat --system sectors --system canonical-log --system f3/ohshitstore --system diversityFilter --system f3 --system ffiwrapper --system main --system f3/gpbft --system bs:sprmgr --system dht/RtRefreshManager --system drand --system f3/wal --system blockstore --system routing/composable --system bitswap_network --system rand --system market_adapter --system fsutil --system evtsm --system autorelay --system preseal --system node --system system --system p2p-holepunch --system messagepool --system watchdog --system metrics-prometheus --system ping --system reuseport-transport --system resources --system dht/netsize --system fil-consensus --system metrics --system events --system p2p-config --system dht --system net/conngater --system vm --system auth --system webrtc-transport --system badger --system rcmgr --system tcp-tpt --system retry --system upgrader --system statemgr --system conngater --system f3/blssig --system rpc warn
    lotus log set-level --system panic-reporter --system incrt --system bitswap-client --system table --system pubsub --system test-logger --system routedhost --system f3/internal/caching --system engine --system badgerbs --system chainstore --system genesis --system messagesigner --system sqlite --system providers --system miner --system f3/certexchange --system cliutil --system lotus-tracer --system fullnode --system gen --system tarutil --system ipns --system websocket-transport --system cli --system stores --system paramfetch --system amt --system splitstore --system blockservice --system webrtc-transport-pion --system build --system ctladdr --system pstoremanager --system quic-utils --system repo --system wallet-ledger --system lock --system dht.pb --system blankhost --system mocknet --system chainindex --system hello --system httpreader --system build/buildtypes --system tracing --system advmgr --system webrtc-udpmux --system paych --system healthcheck --system beacon --system statetree --system bundle --system connmgr --system swarm2 --system chainxchg --system chain --system harmonydb --system peerstore --system net/identify --system autonatv2 --system relay --system fsjournal --system peermgr --system builder --system alerting --system webtransport --system bs:peermgr --system merkledag --system discovery-backoff --system basichost --system disputer --system storageminer --system backupds --system rpcenc --system pathresolv --system peerstore/ds --system sub --system bs:sess --system consensus-common --system f3/manifest-provider --system wallet --system api_proxy --system wdpost --system eventlog --system types --system autonat --system p2p-circuit --system bitswap-server --system actors --system modules --system bitswap --system ulimit --system pubsub/timecache --system slashsvc --system quic-transport --system p2pnode --system payment-channel-settler --system partialfile --system cborrrpc --system nat --system sectors --system canonical-log --system f3/ohshitstore --system diversityFilter --system f3 --system ffiwrapper --system main --system f3/gpbft --system bs:sprmgr --system dht/RtRefreshManager --system drand --system f3/wal --system blockstore --system routing/composable --system bitswap_network --system rand --system market_adapter --system fsutil --system evtsm --system autorelay --system preseal --system node --system system --system p2p-holepunch --system messagepool --system watchdog --system metrics-prometheus --system ping --system reuseport-transport --system resources --system dht/netsize --system fil-consensus --system metrics --system events --system p2p-config --system dht --system net/conngater --system vm --system auth --system webrtc-transport --system badger --system rcmgr --system tcp-tpt --system retry --system upgrader --system statemgr --system conngater --system f3/blssig --system rpc error
    
    # start daemon with genesis creation. only make genesis config if lotus0 is initializing
    if [ "$no" -eq 0 ]; then
        echo "Lotus$no: starting daemon with genesis"

        # is this flaky?
        lotus --repo="${LOTUS_PATH}" daemon --lotus-make-genesis=${SHARED_CONFIGS}/devgen.car --genesis-template=${SHARED_CONFIGS}/localnet.json --bootstrap=false --config=config.toml&
    else
        echo "Lotus$no: starting daemon with genesis"
        lotus --repo="${LOTUS_PATH}" daemon --genesis=${SHARED_CONFIGS}/devgen.car --bootstrap=false --config=config.toml&
    fi
else
    echo "lotus${no}: running in daemon-only without genesis"
    lotus --repo="${LOTUS_PATH}" daemon --bootstrap=false --config=config.toml&
fi


# Common post-startup steps
lotus --version
lotus wait-api
echo "lotus${no}: finished waiting for API, proceeding with network setup."

lotus net listen > ${LOTUS_DATA_DIR}/ipv4addr
cat ${LOTUS_DATA_DIR}/ipv4addr | awk 'NR==1 {print; exit}' > ${LOTUS_DATA_DIR}/lotus${no}-ipv4addr
lotus net id > ${LOTUS_DATA_DIR}/lotus${no}-p2pID
lotus auth create-token --perm admin > ${LOTUS_DATA_DIR}/lotus${no}-jwt


# Connect to peers with retries
retries=6
for peer in "${LOTUS_DATA_DIR}/lotus${no}-ipv4addr" "${FOREST_0_DATA_DIR}/forest-listen-addr"; do
    if [ -f "$peer" ]; then
        attempt=1
        while [ $attempt -le $retries ]; do
            echo "lotus${no}: Attempting to connect to peer from $peer (attempt $attempt/$retries)"
            if lotus net connect $(cat $peer); then
                echo "lotus${no}: Successfully connected to peer from $peer"
                break
            fi
            echo "lotus${no}: Failed to connect to peer from $peer"
            attempt=$((attempt + 1))
            sleep 5
        done
    else
        echo "lotus${no}: Peer address file $peer not found"
    fi
done


sleep infinity
