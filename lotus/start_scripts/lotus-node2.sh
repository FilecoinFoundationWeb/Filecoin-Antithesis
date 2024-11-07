#!/bin/bash

# Waiting for drand dkg to be executed
# drand_1_ready=0
# while [[ ${drand_1_ready} -eq 0 ]]
# do
#     echo "lotus-node: checking if drand-1 is ready.."
#     if [[ -e "/container_ready/drand-1" ]]
#     then
#         echo "lotus-node: drand-1 is ready!"
#         echo "lotus-node: continuing startup..."
#         drand_1_ready=1
#         break
#     fi
#     sleep 5
# done
# export LOTUS_F3_BOOTSTRAP_EPOCH=901
# # Waiting for chain_info to be good
# tries=10
# while [ "$tries" -gt 0 ]; do
#     curl 10.20.20.21/info | jq -c
#     chain_info_status=$?
#     if [ $chain_info_status -eq 0 ];
#     then
#         echo "lotus-node: chain_info is ready!"
#         echo " lotus-node: continuing startup..."
#         break
#     fi
#     sleep 3
#     tries=$(( tries - 1 ))
#     echo "$tries connection attempts remaining..."
# done

sleep 90
export LOTUS_F3_BOOTSTRAP_EPOCH=901
export DRAND_CHAIN_INFO=chain_info
export LOTUS_PATH=${LOTUS_2_PATH}
export LOTUS_MINER_PATH=${LOTUS_2_MINER_PATH}
export LOTUS_SKIP_GENESIS_CHECK=_yes_ 
export CGO_CFLAGS_ALLOW="-D__BLST_PORTABLE__" 
export CGO_CFLAGS="-D__BLST_PORTABLE__" 
curl 10.20.20.21/info | jq -c > chain_info
export DRAND_CHAIN_INFO=chain_info
cat ${LOTUS_DATA_DIR}/ipv4addr | awk 'NR==1 {print; exit}' > ${LOTUS_DATA_DIR}/lotus-1-ipv4addr
cat ${LOTUS_DATA_DIR}/lotus-1-ipv4addr
echo "AAAAAA"
echo $LOTUS_PATH
lotus --version
cp /root/.genesis-sectors2/pre-seal-t01001.key ${LOTUS_2_DATA_DIR}/key2
cp /lotus/config-node2.toml "${LOTUS_2_DATA_DIR}/config-node2.toml"
cat localnet.json | jq -r '.NetworkName' > ${LOTUS_2_DATA_DIR}/network_name2
cp localnet2.json ${LOTUS_2_DATA_DIR}/localnet2.json
lotus --repo="${LOTUS_2_PATH}" daemon --genesis=${LOTUS_DATA_DIR}/devgen.car --genesis-template=localnet2.json --bootstrap=false --config=${LOTUS_2_DATA_DIR}/config-node2.toml&
lotus wait-api
lotus net listen > ${LOTUS_2_DATA_DIR}/ipv4addr-node2
lotus net id > ${LOTUS_2_DATA_DIR}/p2pID-node2
lotus auth create-token --perm admin > ${LOTUS_2_DATA_DIR}/jwt-node2
#lotus net connect $(cat ${LOTUS_DATA_DIR}/lotus-1-ipv4addr)
#lotus sync wait
sleep infinity
