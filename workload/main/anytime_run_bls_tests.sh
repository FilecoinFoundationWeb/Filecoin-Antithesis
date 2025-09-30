#!/bin/bash

NODE_NAMES=("Lotus1" "Lotus2")

echo "Starting parallel BLS tests"

for node in "${NODE_NAMES[@]}"; do
    /opt/antithesis/app contracts deploy-blsprecompile --node "$node"
done

echo "Parallel BLS tests completed"