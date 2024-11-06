#!/bin/bash

# Waiting for drand dkg to be executed
drand_1_ready=0
while [[ ${drand_1_ready} -eq 0 ]]
do
    echo "lotus-node: checking if drand-1 is ready.."
    if [[ -e "/container_ready/drand-1" ]]
    then
        echo "lotus-node: drand-1 is ready!"
        echo "lotus-node: continuing startup..."
        drand_1_ready=1
        break
    fi
    sleep 5
done
export LOTUS_F3_BOOTSTRAP_EPOCH=901
# Waiting for chain_info to be good
tries=10
while [ "$tries" -gt 0 ]; do
    curl 10.20.20.21/info | jq -c
    chain_info_status=$?
    if [ $chain_info_status -eq 0 ];
    then
        echo "lotus-node: chain_info is ready!"
        echo " lotus-node: continuing startup..."
        break
    fi
    sleep 3
    tries=$(( tries - 1 ))
    echo "$tries connection attempts remaining..."
done

curl 10.20.20.21/info | jq -c > chain_info
export DRAND_CHAIN_INFO=chain_info
lotus --version
cp /root/.genesis-sectors/pre-seal-t01000.key ${LOTUS_DATA_DIR}/key
cp /lotus/config.toml "${LOTUS_DATA_DIR}/config.toml"
cat localnet.json | jq -r '.NetworkName' > ${LOTUS_DATA_DIR}/network_name
cp localnet.json ${LOTUS_DATA_DIR}/localnet.json
lotus daemon --lotus-make-genesis=${LOTUS_DATA_DIR}/devgen.car --genesis-template=localnet.json --bootstrap=false --config=${LOTUS_DATA_DIR}/config.toml&
lotus wait-api

echo Finished waiting for API, importing wallet now.

lotus net listen > ${LOTUS_DATA_DIR}/ipv4addr
lotus net id > ${LOTUS_DATA_DIR}/p2pID
lotus auth create-token --perm admin > ${LOTUS_DATA_DIR}/jwt

touch /container_ready/lotus-node

sleep infinity
