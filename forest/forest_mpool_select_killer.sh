#!/usr/bin/env bash

# Fetch the last 100 tipsets from forest-cli and construct JSON-RPC requests for MpoolSelect method

echo "Running regression test for forest MessagePool panic"

export TOKEN=$(cat ${FOREST_DATA_DIR}/jwt)
export FULLNODE_API_INFO=$TOKEN:/ip4/${FOREST_IP}/tcp/${FOREST_RPC_PORT}/http
echo "FULLNODE_API_INFO: $FULLNODE_API_INFO"

forest-cli chain head --format json -n 100 | jq -c '.[] | {
  jsonrpc: "2.0",
  method: "Filecoin.MpoolSelect",
  params: [(.cids | map({"/": .})), 0.8],
  id: 1
}'

echo "Finished running regression test for forest MessagePool panic"