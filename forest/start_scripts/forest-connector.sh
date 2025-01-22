#!/bin/bash

# url="http://10.0.0.28:3456"
# max_attempts=10
# attempts=0

# while true; do
#     http_response=$(curl "$url")

#     if [[ "$http_response" -eq 200 ]]; then
#         echo "Connection successful to forest!"
#         break
#     fi

#     ((attempts++))
#     if [ "$attempts" -ge "$max_attempts" ]; then
#         echo "Max attempts reached. Exiting."
#         break
#     fi

#     echo "Connection failed. Retrying... (Attempt $attempts/$max_attempts)"
#     sleep 3
# done

sleep 30

set -euxo pipefail
export TOKEN=$(cat ${FOREST_DATA_DIR}/jwt)
export FULLNODE_API_INFO=$TOKEN:/ip4/${FOREST_IP}/tcp/${FOREST_RPC_PORT}/http
echo "FULLNODE_API_INFO: $FULLNODE_API_INFO"

forest-wallet --remote-wallet import ${LOTUS_1_DATA_DIR}/key
forest-wallet new bls

forest-cli net connect $(cat ${LOTUS_1_DATA_DIR}/lotus-1-ipv4addr)
forest-cli sync wait
echo "Done"
exit 0