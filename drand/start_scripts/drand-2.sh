#!/bin/bash

# Generate the key pair for the second node
drand generate-keypair --scheme bls-unchained-g1-rfc9380 --id default 10.20.20.22:8080 
# Start the drand daemon for the second node
drand start --private-listen 10.20.20.22:8080 --control 127.0.0.1:8888 --public-listen 0.0.0.0:80 &

echo "SETUP: Node 2 ready, joining DKG as a follower"

# Waiting for dkg initial proposal to be available
tries=10
while [ "$tries" -gt 0 ]; do
    echo "drand-2: checking dkg status"
    lines=$(drand dkg status --control 8888 | wc -l)
    if [ "$lines" -gt 10 ]; then
        echo "drand-2: dkg status up"
        break
    fi
    tries=$(( tries - 1 ))
    echo "drand-2: $tries connection attempts remaining..."
    sleep 1
done

if [ "$tries" -eq 0 ]; then
    echo "drand-2: dkg status never good"
    exit 1
fi
# Join the DKG process initiated by the leader
drand dkg join --control 8888

sleep infinity
