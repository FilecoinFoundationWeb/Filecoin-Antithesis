#!/bin/bash

drand_1_ready=0
while [[ ${drand_1_ready?} -eq 0 ]]
do
    echo "lotus-node: checking if drand-1 is ready.."
    if [[ -e "/container_ready/drand-1" ]]
    then
        echo "lotus-node: drand-1 is ready!"
        echo "lotus-node: continuing startup..."
        drand_1_ready=1
    fi
    sleep 10
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
ls -l ${LOTUS_DATA_DIR}
lotus auth create-token --perm admin > ${LOTUS_DATA_DIR}/jwt

touch /container_ready/lotus-node

sleep infinity

