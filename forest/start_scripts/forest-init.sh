#!/bin/bash
sleep 5

FIRST_RUN_FLAG="${FOREST_DATA_DIR}/.initialized"

if [ ! -f "$FIRST_RUN_FLAG" ]; then
  echo "forest: first startup – performing one-time setup…"
  DRAND_SERVER="http://10.20.20.21"
  json=$(curl -s "$DRAND_SERVER/info")
  formatted_json=$(jq --arg server "$DRAND_SERVER" '{ "servers": [$server], "chain_info": { "public_key": .public_key, "period": .period, "genesis_time": .genesis_time, "hash": .hash, "groupHash": .groupHash }, "network_type": "Quicknet" }' <<<"$json")
  echo "formatted_json: $formatted_json"
  export FOREST_DRAND_QUICKNET_CONFIG="$formatted_json"
  NETWORK_NAME=$(jq -r '.NetworkName' "${LOTUS_1_DATA_DIR}/localnet.json")
  export NETWORK_NAME=$NETWORK_NAME
  forest --version
  cp /forest/forest_config.toml.tpl "${FOREST_DATA_DIR}/forest_config.toml"
  echo "name = \"${NETWORK_NAME}\"" >> "${FOREST_DATA_DIR}/forest_config.toml"
  forest --genesis "${LOTUS_1_DATA_DIR}/devgen.car" \
         --config "${FOREST_DATA_DIR}/forest_config.toml" \
         --save-token "${FOREST_DATA_DIR}/jwt" \
         --rpc-address ${FOREST_IP}:${FOREST_RPC_PORT} \
         --p2p-listen-address /ip4/${FOREST_IP}/tcp/${FOREST_P2P_PORT} \
         --healthcheck-address ${FOREST_IP}:${FOREST_HEALTHZ_RPC_PORT} \
         --skip-load-actors &
  sleep 10
  touch "$FIRST_RUN_FLAG"
  echo "forest: one-time setup complete."
else
  echo "forest: skipping one-time setup."
  DRAND_SERVER="http://10.20.20.21"
  json=$(curl -s "$DRAND_SERVER/info")
  formatted_json=$(jq --arg server "$DRAND_SERVER" '{ "servers": [$server], "chain_info": { "public_key": .public_key, "period": .period, "genesis_time": .genesis_time, "hash": .hash, "groupHash": .groupHash }, "network_type": "Quicknet" }' <<<"$json")
  export FOREST_DRAND_QUICKNET_CONFIG="$formatted_json"
  NETWORK_NAME=$(jq -r '.NetworkName' "${LOTUS_1_DATA_DIR}/localnet.json")
  export NETWORK_NAME=$NETWORK_NAME
  echo "forest: starting forest..."
  forest --genesis "${LOTUS_1_DATA_DIR}/devgen.car" \
         --config "${FOREST_DATA_DIR}/forest_config.toml" \
         --save-token "${FOREST_DATA_DIR}/jwt" \
         --rpc-address ${FOREST_IP}:${FOREST_RPC_PORT} \
         --p2p-listen-address /ip4/${FOREST_IP}/tcp/${FOREST_P2P_PORT} \
         --healthcheck-address ${FOREST_IP}:${FOREST_HEALTHZ_RPC_PORT} \
         --skip-load-actors &
  sleep 10
fi
export TOKEN=$(cat ${FOREST_DATA_DIR}/jwt)
export FULLNODE_API_INFO=$TOKEN:/ip4/${FOREST_IP}/tcp/${FOREST_RPC_PORT}/http
echo "FULLNODE_API_INFO: $FULLNODE_API_INFO"
echo "forest: collecting network info…"
forest-cli net listen | head -n1 > "${FOREST_DATA_DIR}/forest-listen-addr"
echo "forest: connecting to lotus nodes…"
forest-wallet --remote-wallet import ${LOTUS_1_DATA_DIR}/key || true
forest-wallet --remote-wallet import ${LOTUS_2_DATA_DIR}/key || true
forest-cli net connect "$(cat ${LOTUS_1_DATA_DIR}/lotus-1-ipv4addr)"
forest-cli net connect "$(cat ${LOTUS_2_DATA_DIR}/lotus-2-ipv4addr)"

forest-cli sync wait
echo "forest: ready."
sleep infinity
