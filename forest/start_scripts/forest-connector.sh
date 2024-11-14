#!/bin/bash

sleep 25

# Waiting for forest node to be up
forest_ready=0
while [[ ${forest_ready?} -eq 0 ]]
do
   echo "forest-connector: checking if forest is ready.."
   if [[ -e "/container_ready/forest" ]]; then
       echo "forest-connector: forest is ready!"
       echo "forest-connector: continuing startup..."
       forest_ready=1
   fi
   sleep 5
done

set -euxo pipefail
export TOKEN=$(cat ${FOREST_DATA_DIR}/token.jwt)
export FULLNODE_API_INFO=$TOKEN:/ip4/${FOREST_IP}/tcp/${FOREST_RPC_PORT}/http
echo "FULLNODE_API_INFO: $FULLNODE_API_INFO"

forest-wallet --remote-wallet import ${LOTUS_1_DATA_DIR}/key
forest-wallet new bls

forest-cli net connect $(cat ${LOTUS_1_DATA_DIR}/lotus-1-ipv4addr)
forest-cli sync wait
echo "Done"
