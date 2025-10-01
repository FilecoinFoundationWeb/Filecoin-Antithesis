#!/bin/bash

no="$1"

lotus_path="LOTUS_${no}_PATH"
export LOTUS_PATH="${!lotus_path}"

lotus_data_dir="LOTUS_${no}_DATA_DIR"
export LOTUS_DATA_DIR="${!lotus_data_dir}"

lotus_ip="LOTUS_${no}_IP"
LOTUS_IP="${!lotus_ip}"

lotus_rpc_port="LOTUS_${no}_RPC_PORT"
LOTUS_RPC_PORT="${!lotus_rpc_port}"


# Common environment setup, delete all these? what are these even used for?
export LOTUS_F3_BOOTSTRAP_EPOCH=21
export LOTUS_MINER_PATH=${LOTUS_MINER_1_PATH}
export LOTUS_SKIP_GENESIS_CHECK=${LOTUS_SKIP_GENESIS_CHECK}


export CGO_CFLAGS_ALLOW="-D__BLST_PORTABLE__"
export CGO_CFLAGS="-D__BLST_PORTABLE__"
export LOTUS_CHAININDEXER_ENABLEINDEXER=true


# check if initialization is needed
if [ ! -f "${LOTUS_DATA_DIR}/config.toml" ]; then
    INIT_MODE=true
    echo "lotus${no}: First run detected, performing initialization..."
else
    INIT_MODE=false
    echo "lotus${no}: Found existing setup, running in daemon-only mode..."
fi


#TODO: I don't think this works. DRAND_CHAIN_INFO is empty and I think that results in errors later on. How can we curl something to check that drand is ready? I dont know.. I think we had a sleep 5 seconds before, maybe that will work, but that also bad practice
# get a fresh chain info
if [ "$INIT_MODE" = "true" ]; then
    retries=10
    while [ "$retries" -gt 0 ]; do
        curl 10.20.20.21/info | jq -c
        chain_info_status=$?
        if [ $chain_info_status -eq 0 ];
        then
            echo "---------------"
            echo "${chain_info_status}"
            echo "---------------"

            $chain_info_status > chain_info
            export DRAND_CHAIN_INFO=chain_info
            
            echo "---------------"
            echo "${DRAND_CHAIN_INFO}"
            echo "---------------"

            break
        fi
        sleep 3
        retries=$(( tries - 1 ))
        echo "lotus${no}: $retries connection attempts remaining..."
    done
fi


# Initialization steps
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
    
    # Set log levels for initialization
    lotus log set-level --system panic-reporter --system incrt --system bitswap-client --system table --system pubsub --system test-logger --system routedhost --system f3/internal/caching --system engine --system badgerbs --system chainstore --system genesis --system messagesigner --system sqlite --system providers --system miner --system f3/certexchange --system cliutil --system lotus-tracer --system fullnode --system gen --system tarutil --system ipns --system websocket-transport --system cli --system stores --system paramfetch --system amt --system splitstore --system blockservice --system webrtc-transport-pion --system build --system ctladdr --system pstoremanager --system quic-utils --system repo --system wallet-ledger --system lock --system dht.pb --system blankhost --system mocknet --system chainindex --system hello --system httpreader --system build/buildtypes --system tracing --system advmgr --system webrtc-udpmux --system paych --system healthcheck --system beacon --system statetree --system bundle --system connmgr --system swarm2 --system chainxchg --system chain --system harmonydb --system peerstore --system net/identify --system autonatv2 --system relay --system fsjournal --system peermgr --system builder --system alerting --system webtransport --system bs:peermgr --system merkledag --system discovery-backoff --system basichost --system disputer --system storageminer --system backupds --system rpcenc --system pathresolv --system peerstore/ds --system sub --system bs:sess --system consensus-common --system f3/manifest-provider --system wallet --system api_proxy --system wdpost --system eventlog --system types --system autonat --system p2p-circuit --system bitswap-server --system actors --system modules --system bitswap --system ulimit --system pubsub/timecache --system slashsvc --system quic-transport --system p2pnode --system payment-channel-settler --system partialfile --system cborrrpc --system nat --system sectors --system canonical-log --system f3/ohshitstore --system diversityFilter --system f3 --system ffiwrapper --system main --system f3/gpbft --system bs:sprmgr --system dht/RtRefreshManager --system drand --system f3/wal --system blockstore --system routing/composable --system bitswap_network --system rand --system market_adapter --system fsutil --system evtsm --system autorelay --system preseal --system node --system system --system p2p-holepunch --system messagepool --system watchdog --system metrics-prometheus --system ping --system reuseport-transport --system resources --system dht/netsize --system fil-consensus --system metrics --system events --system p2p-config --system dht --system net/conngater --system vm --system auth --system webrtc-transport --system badger --system rcmgr --system tcp-tpt --system retry --system upgrader --system statemgr --system conngater --system f3/blssig --system rpc warn
    lotus log set-level --system panic-reporter --system incrt --system bitswap-client --system table --system pubsub --system test-logger --system routedhost --system f3/internal/caching --system engine --system badgerbs --system chainstore --system genesis --system messagesigner --system sqlite --system providers --system miner --system f3/certexchange --system cliutil --system lotus-tracer --system fullnode --system gen --system tarutil --system ipns --system websocket-transport --system cli --system stores --system paramfetch --system amt --system splitstore --system blockservice --system webrtc-transport-pion --system build --system ctladdr --system pstoremanager --system quic-utils --system repo --system wallet-ledger --system lock --system dht.pb --system blankhost --system mocknet --system chainindex --system hello --system httpreader --system build/buildtypes --system tracing --system advmgr --system webrtc-udpmux --system paych --system healthcheck --system beacon --system statetree --system bundle --system connmgr --system swarm2 --system chainxchg --system chain --system harmonydb --system peerstore --system net/identify --system autonatv2 --system relay --system fsjournal --system peermgr --system builder --system alerting --system webtransport --system bs:peermgr --system merkledag --system discovery-backoff --system basichost --system disputer --system storageminer --system backupds --system rpcenc --system pathresolv --system peerstore/ds --system sub --system bs:sess --system consensus-common --system f3/manifest-provider --system wallet --system api_proxy --system wdpost --system eventlog --system types --system autonat --system p2p-circuit --system bitswap-server --system actors --system modules --system bitswap --system ulimit --system pubsub/timecache --system slashsvc --system quic-transport --system p2pnode --system payment-channel-settler --system partialfile --system cborrrpc --system nat --system sectors --system canonical-log --system f3/ohshitstore --system diversityFilter --system f3 --system ffiwrapper --system main --system f3/gpbft --system bs:sprmgr --system dht/RtRefreshManager --system drand --system f3/wal --system blockstore --system routing/composable --system bitswap_network --system rand --system market_adapter --system fsutil --system evtsm --system autorelay --system preseal --system node --system system --system p2p-holepunch --system messagepool --system watchdog --system metrics-prometheus --system ping --system reuseport-transport --system resources --system dht/netsize --system fil-consensus --system metrics --system events --system p2p-config --system dht --system net/conngater --system vm --system auth --system webrtc-transport --system badger --system rcmgr --system tcp-tpt --system retry --system upgrader --system statemgr --system conngater --system f3/blssig --system rpc error
    
    # Start daemon with genesis creation. Only make genesis config if lotus0 is initializing
    if [ "$no" -eq 0 ]; then
        echo "Lotus$no: starting darmon with genesis"
        #lotus --repo="${LOTUS_PATH}" daemon --lotus-make-genesis=${SHARED_CONFIGS}/devgen.car --genesis-template=${SHARED_CONFIGS}/localnet.json --bootstrap=false --config=config.toml&
        lotus daemon --lotus-make-genesis=${SHARED_CONFIGS}/devgen.car --genesis-template=${SHARED_CONFIGS}/localnet.json --bootstrap=false --config=config.toml&
    else
        echo "Lotus$no: starting regular lotus daemon"
        lotus --repo="${LOTUS_PATH}" daemon --bootstrap=false --config=config.toml&
    fi
else
    echo "lotus${no}: running in daemon-only mode..."
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
retries=5
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
