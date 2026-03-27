#!/bin/bash

SECTOR_SIZE="${SECTOR_SIZE:-2KiB}"
NETWORK_NAME="${NETWORK_NAME:-2k}"

# Clean stale data from previous runs so miners re-init with the new genesis.
# Without this, miner repos from a prior run keep old sector proofs that don't
# match the freshly-generated genesis state, causing "faulty sectors" errors.
echo "Cleaning stale genesis artifacts and miner repos..."
rm -rf "${SHARED_CONFIGS}"/.genesis-sector-*
rm -f  "${SHARED_CONFIGS}/manifest.json" "${SHARED_CONFIGS}/localnet.json" "${SHARED_CONFIGS}/devgen.car"

for ((i=0; i<NUM_LOTUS_MINERS; i++)); do
    _mp_var="LOTUS_MINER_${i}_PATH"
    _mp="${!_mp_var}"
    if [ -n "$_mp" ] && [ -d "$_mp" ]; then
        echo "  removing stale miner repo: $_mp"
        rm -rf "$_mp"
    fi
done
for ((i=0; i<${NUM_LOTUS_ADVERSARIES:-0}; i++)); do
    _mp_var="LOTUS_ADVERSARY_MINER_${i}_PATH"
    _mp="${!_mp_var}"
    if [ -n "$_mp" ] && [ -d "$_mp" ]; then
        echo "  removing stale adversary miner repo: $_mp"
        rm -rf "$_mp"
    fi
done

# pre-seal each miner with configurable sector counts
# SECTORS_PER_MINER is a comma-separated list (e.g. "4,2"), defaults to 2 per miner
IFS=',' read -ra _sector_counts <<< "${SECTORS_PER_MINER:-}"

for ((i=0; i<NUM_LOTUS_MINERS; i++)); do
  miner_id=$(printf "t01%03d" "$i")
  sector_dir="${SHARED_CONFIGS}/.genesis-sector-${i}"
  num_sectors="${_sector_counts[$i]:-2}"
  echo "Pre-sealing miner $miner_id into $sector_dir (${num_sectors} sectors)"
  lotus-seed --sector-dir="$sector_dir" pre-seal --sector-size "$SECTOR_SIZE" --num-sectors "$num_sectors" --miner-addr "$miner_id"
done

# pre-seal adversary miners — actor addresses follow immediately after regular miners
IFS=',' read -ra _adversary_sector_counts <<< "${ADVERSARY_SECTORS_PER_MINER:-}"

for ((i=0; i<NUM_LOTUS_ADVERSARIES; i++)); do
  miner_id=$(printf "t01%03d" "$((NUM_LOTUS_MINERS + i))")
  sector_dir="${SHARED_CONFIGS}/.genesis-sector-adversary-${i}"
  num_sectors="${_adversary_sector_counts[$i]:-2}"
  echo "Pre-sealing adversary miner $miner_id into $sector_dir (${num_sectors} sectors)"
  lotus-seed --sector-dir="$sector_dir" pre-seal --sector-size "$SECTOR_SIZE" --num-sectors "$num_sectors" --miner-addr "$miner_id"
done

# create initial genesis template
lotus-seed genesis new --network-name="$NETWORK_NAME" ${SHARED_CONFIGS}/localnet.json

echo "Waiting for genesis allocations from Workload container..."
echo "Looking for: ${SHARED_CONFIGS}/genesis_allocs.json"

# Wait up to 60 seconds for the file to appear
MAX_RETRIES=60
count=0
while [ ! -f "${SHARED_CONFIGS}/genesis_allocs.json" ]; do
    sleep 1
    count=$((count+1))
    if [ $count -ge $MAX_RETRIES ]; then
        echo "ERROR: Timed out waiting for genesis_allocs.json"
        exit 1
    fi
    echo "Waiting... ($count/$MAX_RETRIES)"
done

echo "File found! Injecting 100 wallets..."

# Merge using jq
jq --slurpfile allocs ${SHARED_CONFIGS}/genesis_allocs.json \
   '.Accounts += $allocs[]' \
   ${SHARED_CONFIGS}/localnet.json > ${SHARED_CONFIGS}/localnet.tmp \
   && mv ${SHARED_CONFIGS}/localnet.tmp ${SHARED_CONFIGS}/localnet.json

echo "Injection successful."

# aggregate all pre-seal manifests into one
manifest_files=()
for ((i=0; i<NUM_LOTUS_MINERS; i++)); do
  miner_id=$(printf "t01%03d" "$i")
  manifest_files+=("${SHARED_CONFIGS}/.genesis-sector-${i}/pre-seal-${miner_id}.json")
done
for ((i=0; i<NUM_LOTUS_ADVERSARIES; i++)); do
  miner_id=$(printf "t01%03d" "$((NUM_LOTUS_MINERS + i))")
  manifest_files+=("${SHARED_CONFIGS}/.genesis-sector-adversary-${i}/pre-seal-${miner_id}.json")
done

echo "Aggregating manifests..."
lotus-seed aggregate-manifests "${manifest_files[@]}" > ${SHARED_CONFIGS}/manifest.json

lotus-seed genesis add-miner "${SHARED_CONFIGS}/localnet.json" "${SHARED_CONFIGS}/manifest.json"

echo "Genesis setup complete for $NUM_LOTUS_MINERS miner(s) and $NUM_LOTUS_ADVERSARIES adversary miner(s)."
