#!/bin/bash

export KEYSTORE="/opt/antithesis/keystore/keystore.json"
export PASSWORD="password123"
export RPC_URL="http://10.20.20.24:1234/rpc/v1"
export CHAIN_ID="31415926"

/opt/antithesis/app wallet create-eth-keystore --node Lotus1

cd /opt/antithesis/projects/filecoin-pay/tools/
./deploy.sh 31415926