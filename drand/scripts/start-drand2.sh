#!/bin/bash

# Generate the key pair for the third node
drand generate-keypair --scheme bls-unchained-g1-rfc9380 --id default drand2:8080 
# Start the drand daemon for the third node
drand start --private-listen drand2:8080 --control 127.0.0.1:8888 --public-listen 0.0.0.0:80 &

echo "ready! joining DKG as a follower"

# Waiting for dkg initial proposal to be available
tries=10
while [ "$tries" -gt 0 ]; do
    echo "checking dkg status..."
    lines=$(drand dkg status --control 8888 | wc -l)
    if [ "$lines" -gt 10 ]; then
        echo "dkg status up"
        break
    fi
    tries=$(( tries - 1 ))
    echo "$tries connection attempts remaining..."
    sleep 1
done

if [ "$tries" -eq 0 ]; then
    echo "dkg status never good"
    exit 1
fi

# Join the DKG process initiated by the leader
drand dkg join --control 8888 

sleep infinity
