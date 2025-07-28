#!/bin/bash

set -e

echo "Workload [main][first.sh]: initializing wallets..."

python3 -u /opt/antithesis/resources/initialize_wallets.py "forest"

echo "Workload [main][first.sh]: completed workload setup."
