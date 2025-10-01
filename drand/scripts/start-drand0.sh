#!/bin/bash

# Generate the key pair for the leader node
drand generate-keypair --scheme bls-unchained-g1-rfc9380 --id default 10.20.20.21:8080 
# Start the drand daemon for the leader node
drand start --id default --private-listen 10.20.20.21:8080 --control 127.0.0.1:8888 --public-listen 0.0.0.0:80 &

# Wait until drand1 and drand2 are up
tries=10
while [ "$tries" -gt 0 ]; do
    drand util check 10.20.20.22:8080
    drand1_status=$?
    drand util check 10.20.20.23:8080
    drand2_status=$?
    if [ $drand1_status -eq 0 ] && [ $drand2_status -eq 0 ];
    then
        echo "drand0: discovered drand1 and drand2"
        break
    fi
    sleep 1
    tries=$(( tries - 1 ))
    echo "$tries connection attempts remaining..."
done

if [ "$tries" -eq 0 ]; then
    echo "drand0: timed out waiting for drand1 and drand2. exiting."
    exit 1
fi

echo "drand0: ready! initializing DKG as leader"

# Initialize the DKG process as the leader
drand dkg generate-proposal --joiner 10.20.20.21:8080 --joiner 10.20.20.22:8080 --joiner 10.20.20.23:8080 --out proposal.toml
drand dkg init --proposal proposal.toml --threshold 2 --period 3s --scheme bls-unchained-g1-rfc9380 --catchup-period 0s --genesis-delay 30s
# Waiting for other drand nodes to join proposal

drand dkg execute

sleep infinity
