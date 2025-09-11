#!/bin/bash

export TOKEN=$(cat "${FOREST_DATA_DIR}/jwt")
export FULLNODE_API_INFO=$TOKEN:/ip4/${FOREST_IP}/tcp/${FOREST_RPC_PORT}/http

# Get chain head and extract the epoch number from brackets
CHAIN_HEAD_OUTPUT=$(forest-cli chain head 2>/dev/null)
current_epoch=$(echo "$CHAIN_HEAD_OUTPUT" | grep -o '\[[0-9]*\]' | grep -o '[0-9]*')

if [[ -z "$current_epoch" || ! "$current_epoch" =~ ^[0-9]+$ ]]; then
  echo "Could not determine current epoch from chain head. Exiting."
  exit 0
fi

if (( current_epoch < 30 )); then
  echo "Current epoch ($current_epoch) is less than 30. Exiting."
  exit 0
fi

# Check if snapshots directory exists, create if not
SNAPSHOTS_DIR="${FOREST_DATA_DIR}/snapshots"
if [ -f "$SNAPSHOTS_DIR" ]; then
  echo "Snapshots path exists as a file. Removing file: $SNAPSHOTS_DIR"
  rm "$SNAPSHOTS_DIR"
fi
if [ ! -d "$SNAPSHOTS_DIR" ]; then
  echo "Creating snapshots directory: $SNAPSHOTS_DIR"
  mkdir -p "$SNAPSHOTS_DIR"
fi

forest-cli snapshot export --format v2 --output-path ${FOREST_DATA_DIR}/snapshots/






