#!/usr/bin/env bash

# GLOBALS
export FOREST_DATA_DIR=/forest_data
export TOKEN=$(cat "${FOREST_DATA_DIR}/jwt")
export FULLNODE_API_INFO=$TOKEN:/ip4/${FOREST_IP}/tcp/${FOREST_RPC_PORT}/http

# Create a timestamp-based snapshot directory
snapshot_dir="${FOREST_DATA_DIR}/snapshots"
mkdir -p "$snapshot_dir"
timestamp=$(date +%Y%m%d_%H%M%S)
snapshot_path="${snapshot_dir}/snapshot_${timestamp}"

echo "Exporting snapshot to: $snapshot_path"

# Export the snapshot using forest-cli with v2 format
if forest-cli snapshot export --format v2 --output-path "$snapshot_path"; then
    echo "Snapshot exported successfully to $snapshot_path"
    
    # Validate the exported snapshot
    echo "Validating exported snapshot..."
    if forest-tool snapshot validate "$snapshot_path"; then
        # Check the output for verification status
        validation_output=$(forest-tool snapshot validate "$snapshot_path" 2>&1)
        if echo "$validation_output" | grep -q "IPLD integrity.*✅.*verified" && \
           echo "$validation_output" | grep -q "genesis block.*✅.*found"; then
            echo "✅ Snapshot validation successful!"
            echo "  - IPLD integrity: verified"
            echo "  - Genesis block: found"
        else
            echo "❌ Snapshot validation failed or incomplete"
            echo "$validation_output"
        fi
    else
        echo "❌ Snapshot validation failed"
    fi
else
    echo "❌ Snapshot export failed"
fi