#!/usr/bin/env bash

# Fetch the last 100 tipsets from forest-cli and construct JSON-RPC requests for MpoolSelect method

echo "Running regression test for forest MessagePool panic"
# https://github.com/ChainSafe/forest/issues/4490

export TOKEN=$(cat ${FOREST_DATA_DIR}/jwt)
export FULLNODE_API_INFO=$TOKEN:/ip4/${FOREST_IP}/tcp/${FOREST_RPC_PORT}/http
export FOREST_RPC_URL="http://${FOREST_IP}:${FOREST_RPC_PORT}/rpc/v0"
#echo "FULLNODE_API_INFO: $FULLNODE_API_INFO"
#echo "FOREST_RPC_URL: $FOREST_RPC_URL"

MAX_TIPSETS=50

CURRENT_HEIGHT=$(forest-cli chain head --format json | jq '.[0].epoch')
echo "Current height: $CURRENT_HEIGHT"
if [ "$CURRENT_HEIGHT" -lt 1 ]; then
  echo "Chain head is at 0. Exiting script.."
  exit 1
elif [ "$CURRENT_HEIGHT" == "null" ]; then
  echo "Could not get chain head. Exiting script.."
  exit 1
else
  echo "Running script.."
fi

if [ "$CURRENT_HEIGHT" -gt "$MAX_TIPSETS" ]; then
  NUM_TIPSETS=$MAX_TIPSETS
else
  NUM_TIPSETS=$CURRENT_HEIGHT
fi

TIPSETS=$(forest-cli chain head --format json -n "$NUM_TIPSETS")

# Get the tipsets and send MpoolSelect requests for each
echo "$TIPSETS" | jq -c '.[] | { cids: .cids }' | \
while read -r line; do
  CIDS=$(echo "$line" | jq -c '[.cids[] | {"/": .}]')
  JSON=$(jq -n --argjson cids "$CIDS" --argjson id 1 '{
    jsonrpc: "2.0",
    method: "Filecoin.MpoolSelect",
    params: [$cids, 0.8],
    id: $id
  }')

  #echo "Sending request: $JSON"

  curl -sS -X POST "$FOREST_RPC_URL" \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer $TOKEN" \
    -d "$JSON" || echo "Request failed"

done

echo #add new line before
echo "Finished running regression test for forest MessagePool panic"
exit 0