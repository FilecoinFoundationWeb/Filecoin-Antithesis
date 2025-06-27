#!/usr/bin/env bash

# Fetch the last 100 tipsets from forest-cli and construct JSON-RPC requests for MpoolSelect method

echo "Running regression test for forest MessagePool panic"

export TOKEN=$(cat ${FOREST_DATA_DIR}/jwt)
export FULLNODE_API_INFO=$TOKEN:/ip4/${FOREST_IP}/tcp/${FOREST_RPC_PORT}/http
export FOREST_RPC_URL=${FOREST_IP}:${FOREST_RPC_PORT} 
echo "FULLNODE_API_INFO: $FULLNODE_API_INFO"

# Get the last 100 tipsets and send MpoolSelect requests for each
forest-cli chain head --format json -n 100 | \
jq -c '.[] | { cids: .cids }' | \
while read -r line; do
  CIDS=$(echo "$line" | jq -c '[.cids[] | {"\/": .}]')
  JSON=$(jq -n --argjson cids "$CIDS" --argjson id 1 '{
    jsonrpc: "2.0",
    method: "Filecoin.MpoolSelect",
    params: [$cids, 0.8],
    id: $id
  }')

  echo "Sending request: $JSON"

  curl -sS -X POST "$FOREST_RPC_URL" \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer $TOKEN" \
    -d "$JSON" || echo "Request failed"

done

echo "Finished running regression test for forest MessagePool panic"