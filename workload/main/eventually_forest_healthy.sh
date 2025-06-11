#!/bin/bash

FOREST_IP="10.20.20.28"
PORT="2346"
ENDPOINT="/healthz?verbose"
URL="http://${FOREST_IP}:${PORT}${ENDPOINT}"

# Antithesis SDK output path
output_path="${ANTITHESIS_OUTPUT_DIR}/sdk.jsonl"

# initial SDK assertion (assume healthy)
echo '{"antithesis_assert": {
    "hit": false, 
    "must_hit": true, 
    "assert_type": "always", 
    "display_type": "Always", 
    "message": "Forest node stays healthy", 
    "condition": false, 
    "id": "Forest node stays healthy", 
    "location": {}, 
    "details": null
    }}' >> "$output_path"

# initial SDK assertion (assume reachable)
echo '{"antithesis_assert": {
    "hit": false, 
    "must_hit": true, 
    "assert_type": "always", 
    "display_type": "Always", 
    "message": "Forest node stays reachable", 
    "condition": false, 
    "id": "Forest node stays reachable", 
    "location": {}, 
    "details": null
    }}' >> "$output_path"

RESPONSE=$(curl --silent "$URL")

#echo "Response: $RESPONSE"

# Check for unhealthy markers

# empty response then unreachable
if [[ -z "$RESPONSE" ]]; then
  echo "Forest node is unreachable: $RESPONSE"

  # Log assertion failure with details
  echo '{"antithesis_assert": {
    "hit": true,
    "must_hit": true,
    "assert_type": "always",
    "display_type": "Always",
    "message": "Forest node stays reachable",
    "condition": false,
    "id": "Forest node stays reachable",
    "location": {},
    "details": null
  }}' >> "$output_path"

  exit 1
elif echo "$RESPONSE" | grep -q '\[!\]'; then
  echo "Forest node is unhealthy: $RESPONSE"

  # Log assertion failure with details
  echo '{"antithesis_assert": {
    "hit": true,
    "must_hit": true,
    "assert_type": "always",
    "display_type": "Always",
    "message": "Forest node stays healthy",
    "condition": false,
    "id": "Forest node stays healthy",
    "location": {},
    "details": null
  }}' >> "$output_path"

  exit 1
else
  echo "Forest node is healthy: $RESPONSE"
fi
