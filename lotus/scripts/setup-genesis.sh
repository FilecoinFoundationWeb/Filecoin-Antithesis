#!/bin/bash

NUM_LOTUS_CLIENTS=$NUM_LOTUS_CLIENTS
SECTOR_SIZE="${SECTOR_SIZE:-2KiB}"
NETWORK_NAME="${NETWORK_NAME:-2k}"

# pre-seal each miner
for ((i=0; i<NUM_LOTUS_CLIENTS; i++)); do
  miner_id=$(printf "t01%03d" "$i")
  sector_dir="${SHARED_CONFIGS}/.genesis-sector-${i}"
  echo "Pre-sealing miner $miner_id into $sector_dir"
  lotus-seed --sector-dir="$sector_dir" pre-seal --sector-size "$SECTOR_SIZE" --num-sectors 2 --miner-addr "$miner_id"
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
        # Optional: continue without them, or exit 1. 
        # Exiting is safer for deterministic testing.
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
for ((i=0; i<NUM_LOTUS_CLIENTS; i++)); do
  miner_id=$(printf "t01%03d" "$i")
  manifest_files+=("${SHARED_CONFIGS}/.genesis-sector-${i}/pre-seal-${miner_id}.json")
done

echo "Aggregating manifests..."
lotus-seed aggregate-manifests "${manifest_files[@]}" > ${SHARED_CONFIGS}/manifest.json

# is this step flaky/nondeterministic? it was in the Dockerfile. Do we need retries here?
lotus-seed genesis add-miner ${SHARED_CONFIGS}/localnet.json ${SHARED_CONFIGS}/manifest.json

echo "Genesis setup complete for $NUM_LOTUS_CLIENTS miner(s)."
