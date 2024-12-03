#!/bin/bash

echo "lotus-1: waiting for drand dkg to be executed"
drand_1_ready=0
while [[ ${drand_1_ready} -eq 0 ]]
do
    echo "lotus-1: checking if drand-1 is ready.."
    if [[ -e "/container_ready/drand-1" ]]
    then
        echo "lotus-1: drand-1 is ready!"
        echo "lotus-1: continuing startup..."
        drand_1_ready=1
        break
    fi
    sleep 5
done

# Waiting for chain_info to be good
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
export LOTUS_F3_BOOTSTRAP_EPOCH=901
export LOTUS_PATH=${LOTUS_1_PATH}
export LOTUS_MINER_PATH=${LOTUS_MINER_1_PATH}
export LOTUS_SKIP_GENESIS_CHECK=${LOTUS_SKIP_GENESIS_CHECK}
export CGO_CFLAGS_ALLOW="-D__BLST_PORTABLE__"
export CGO_CFLAGS="-D__BLST_PORTABLE__"
export LOTUS_CHAININDEXER_ENABLEINDEXER=true
curl 10.20.20.21/info | jq -c > chain_info
export DRAND_CHAIN_INFO=chain_info
lotus --version
cp /root/.genesis-sector-1/pre-seal-t01000.key ${LOTUS_1_DATA_DIR}/key
cp /lotus/config-1.toml "${LOTUS_1_DATA_DIR}/config.toml"
cat localnet-1.json | jq -r '.NetworkName' > ${LOTUS_1_DATA_DIR}/network_name
cp localnet-1.json ${LOTUS_1_DATA_DIR}/localnet.json
lotus daemon --lotus-make-genesis=${LOTUS_1_DATA_DIR}/devgen.car --genesis-template=${LOTUS_1_DATA_DIR}/localnet.json --bootstrap=false --config=${LOTUS_1_DATA_DIR}/config.toml&
lotus wait-api

echo "lotus-1: finished waiting for API, importing wallet now."

lotus net listen > ${LOTUS_1_DATA_DIR}/ipv4addr
cat ${LOTUS_1_DATA_DIR}/ipv4addr | awk 'NR==1 {print; exit}' > ${LOTUS_1_DATA_DIR}/lotus-1-ipv4addr
lotus net id > ${LOTUS_1_DATA_DIR}/p2pID
lotus auth create-token --perm admin > ${LOTUS_1_DATA_DIR}/jwt
touch /container_ready/lotus-1

sleep infinity
