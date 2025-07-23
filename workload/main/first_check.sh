#!/bin/bash

set -e

echo "Workload [main][first.sh]: initializing wallets..."

python3 -u /opt/antithesis/resources/initialize_wallets.py "forest"
/opt/antithesis/app wallet create --node Lotus1 --count 5
/opt/antithesis/app wallet create --node Lotus2 --count 5

echo "Workload [main][first.sh]: completed workload setup."
