#!/bin/bash
sleep 60
curl 10.20.20.21/info | jq -c > ${LOTUS_DATA_DIR}/chain_info
cat ${LOTUS_DATA_DIR}/chain_info
export DRAND_CHAIN_INFO=${LOTUS_DATA_DIR}/chain_info
ls -la 
if [ ! -f ${LOTUS_DATA_DIR}/NODE_INITIALISED ]; then
    ./lotus-seed --sector-dir ${LOTUS_DATA_DIR}/genesis-sectors pre-seal --sector-size 2KiB --num-sectors 2
    ./lotus-seed --sector-dir ${LOTUS_DATA_DIR}/genesis-sectors genesis new ${LOTUS_DATA_DIR}/localnet.json
    ./lotus-seed --sector-dir ${LOTUS_DATA_DIR}/genesis-sectors genesis add-miner ${LOTUS_DATA_DIR}/localnet.json ${LOTUS_DATA_DIR}/genesis-sectors/pre-seal-${MINER_ACTOR_ADDRESS}.json
    touch ${LOTUS_DATA_DIR}/NODE_INITIALISED
fi
lotus --version
sleep infinity
