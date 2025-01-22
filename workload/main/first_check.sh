#!/bin/bash

set -e

echo "Workload [main][first.sh]: initializing wallets..."

python3 -u /opt/antithesis/resources/initialize_wallets.py "forest"
/opt/antithesis/app -node=Lotus1 -config=/opt/antithesis/resources/config.json -wallets=2 -operation=create
/opt/antithesis/app -node=Lotus2 -config=/opt/antithesis/resources/config.json -wallets=2 -operation=create

echo "Workload [main][first.sh]: completed workload setup."
