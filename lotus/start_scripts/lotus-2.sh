#!/bin/bash



# Waiting for lotus node 1 to be up
lotus_1_ready=0
while [[ ${lotus_1_ready?} -eq 0 ]]
do
    echo "lotus-2: checking if lotus-1 is ready.."
    if [[ -e "/container_ready/lotus-1" ]]
    then
        echo "lotus-2: lotus-1 is ready!"
        echo "lotus-2: continuing startup..."
        lotus_1_ready=1
        break
    fi
    sleep 5
done

export LOTUS_F3_BOOTSTRAP_EPOCH=901
export LOTUS_PATH=${LOTUS_2_PATH}
export LOTUS_MINER_PATH=${LOTUS_MINER_2_PATH}
export LOTUS_SKIP_GENESIS_CHECK=${LOTUS_SKIP_GENESIS_CHECK}
export CGO_CFLAGS_ALLOW="-D__BLST_PORTABLE__" 
export CGO_CFLAGS="-D__BLST_PORTABLE__"
curl 10.20.20.21/info | jq -c > chain_info
export DRAND_CHAIN_INFO=chain_info

lotus --version

# I think all this is redundant
# cat ${LOTUS_1_DATA_DIR}/ipv4addr | awk 'NR==1 {print; exit}' > ${LOTUS_1_DATA_DIR}/lotus-1-ipv4addr
# cat ${LOTUS_1_DATA_DIR}/lotus-1-ipv4addr

cp /root/.genesis-sector-2/pre-seal-t01001.key ${LOTUS_2_DATA_DIR}/key
cp /lotus/config-2.toml "${LOTUS_2_DATA_DIR}/config.toml"
cat localnet-2.json | jq -r '.NetworkName' > ${LOTUS_2_DATA_DIR}/network_name
cp localnet-2.json ${LOTUS_2_DATA_DIR}/localnet.json
lotus --repo="${LOTUS_2_PATH}" daemon --genesis=${LOTUS_1_DATA_DIR}/devgen.car  --bootstrap=false --config=${LOTUS_2_DATA_DIR}/config.toml&
lotus wait-api

lotus net listen > ${LOTUS_2_DATA_DIR}/ipv4addr
lotus net id > ${LOTUS_2_DATA_DIR}/p2pID
lotus auth create-token --perm admin > ${LOTUS_2_DATA_DIR}/jwt
lotus net connect $(cat ${LOTUS_1_DATA_DIR}/ipv4addr)

touch /container_ready/lotus-2

sleep infinity
