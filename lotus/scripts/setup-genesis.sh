#!/bin/bash

no="$1"

NUM_MINERS="${NUM_MINERS:-0}"
SECTOR_SIZE="${SECTOR_SIZE:-2KiB}"
NETWORK_NAME="${NETWORK_NAME:-2k}"

# pre-seal each miner
for ((i=0; i<NUM_MINERS; i++)); do
  miner_id=$(printf "t01%03d" "$i")
  sector_dir="${SHARED_CONFIGS}/.genesis-sector-${i}"
  echo "Pre-sealing miner $miner_id into $sector_dir"
  lotus-seed --sector-dir="$sector_dir" pre-seal --sector-size "$SECTOR_SIZE" --num-sectors 2 --miner-addr "$miner_id"
done

# create initial genesis template
lotus-seed genesis new --network-name="$NETWORK_NAME" ${SHARED_CONFIGS}/localnet.json

# aggregate all pre-seal manifests into one
manifest_files=()
for ((i=0; i<NUM_MINERS; i++)); do
  miner_id=$(printf "t01%03d" "$i")
  manifest_files+=("${SHARED_CONFIGS}/.genesis-sector-${i}/pre-seal-${miner_id}.json")
done

echo "Aggregating manifests..."
lotus-seed aggregate-manifests "${manifest_files[@]}" > ${SHARED_CONFIGS}/manifest.json

# is this step flaky/nondeterministic? it was in the Dockerfile. Do we need retries here?
lotus-seed genesis add-miner ${SHARED_CONFIGS}/localnet.json ${SHARED_CONFIGS}/manifest.json

echo "Genesis setup complete for $NUM_MINERS miner(s)."

./scripts/lotus.sh ${no}