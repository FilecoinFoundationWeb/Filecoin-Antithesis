#!/usr/bin/env bash

# Fetch the last 100 tipsets from forest-cli and construct JSON-RPC requests for MpoolSelect method

echo "Running regression test for forest MessagePool panic"

forest-cli chain head --format json -n 100 | jq -c '.[] | {
  jsonrpc: "2.0",
  method: "Filecoin.MpoolSelect",
  params: [(.cids | map({"/": .})), 0.8],
  id: 1
}'

echo "Finished running regression test for forest MessagePool panic"