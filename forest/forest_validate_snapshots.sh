#!/bin/bash

export FOREST_DATA_DIR=/forest_data
export TOKEN=$(cat "${FOREST_DATA_DIR}/jwt")
export FULLNODE_API_INFO=$TOKEN:/ip4/${FOREST_IP}/tcp/${FOREST_RPC_PORT}/http

forest-tool snapshot validate ${FOREST_DATA_DIR}/snapshots/forest_snapshot_2k_*.forest.car.zst
exit 0