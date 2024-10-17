#!/bin/bash

# Generate the key pair for the leader node
drand generate-keypair --scheme bls-unchained-g1-rfc9380 --id default 10.20.20.21:8080 

# Start the drand daemon for the leader node
drand start --id default --private-listen 10.20.20.21:8080 --control 127.0.0.1:8888 --public-listen 0.0.0.0:80 &

# Wait until drand-2 and drand-3 are up
tries=10
while [ "$tries" -gt 0 ]; do
    drand util check 10.20.20.22:8080
    drand_2_status=$?
    drand util check 10.20.20.23:8080
    drand_3_status=$?
    if [ $drand_2_status -eq 0 ] && [ $drand_3_status -eq 0 ];
    then
        echo "drand-1 found that drand-2 and drand-3 were up"
        break
    fi
    sleep 2
    tries=$(( tries - 1 ))
    echo "$tries connection attempts remaining..."
done

if [ "$tries" -eq 0 ]; then
    echo "drand-1 failed to wait until drand-2 and drand-3 were up"
    exit 1
fi

echo "SETUP: Node 1 ready, initializing DKG as leader"

# Initialize the DKG process as the leader
drand dkg generate-proposal  --joiner 10.20.20.21:8080 --joiner 10.20.20.22:8080 --joiner 10.20.20.23:8080  --out proposal.toml
drand dkg init --proposal ./proposal.toml --threshold 2 --period 3s --scheme bls-unchained-g1-rfc9380 --catchup-period 0s --genesis-delay 30s


# TODO: Struggling to understand how drand-1 knows when drand-2 and drand-3 have joined the proposal. Then execute. Want to fix
# this to remove plain sleep
echo "remove sleep here and wait until others have joined"
sleep 10
drand dkg execute

touch /container_ready/drand-1

sleep infinity
