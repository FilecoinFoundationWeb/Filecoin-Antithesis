#!/bin/bash
file_to_check="/go/lotus-local-net/devgen/devgen.car"
while [ ! -f "$file_to_check" ]; do
    echo "Waiting for $file_to_check to be created..."
    sleep 5
done

chain_1=$(curl 10.20.20.21/chains | jq -r '.[]')
curl 10.20.20.21/$chain_1/info | jq -c  > /go/lotus-local-net/devgen/chain_info
export DRAND_CHAIN_INFO=/go/lotus-local-net/devgen/chain_info

/go/lotus-local-net/./lotus daemon  --genesis=/go/lotus-local-net/devgen/devgen.car --bootstrap=false &
/go/lotus-local-net/./lotus wallet import --as-default ~/.genesis-sectors/pre-seal-t01001.key
/go/lotus-local-net/./lotus-miner init --genesis-miner --actor=t01001 --sector-size=2KiB --pre-sealed-sectors=~/.genesis-sectors --pre-sealed-metadata=~/.genesis-sectors/pre-seal-t01001.json --nosync
/go/lotus-local-net/./lotus-miner run --nosync 

file_to_check="/go/lotus-local-net/devgen/ipv4addr"
while [ ! -f "$file_to_check" ]; do
    echo "Waiting for $file_to_check to be created..."
    sleep 5
done
ipv4addr=$(head -n 1 $file_to_check)
echo $ipv4addr
/go/lotus-local-net/./lotus net connect $ipv4addr
/go/lotus-local-net/.lotus sync status
sleep infinity