#!/usr/bin/env bash

# This script calls the lotus-bench binary from lotus 1


odn=$(od -An -N1 -d /dev/urandom)

mapped=$(( (odn % 4) + 1))

case "$mapped" in
    1) 
        echo "Running benchmark test for lotus-1 with method eth_chainId"
        ./lotus-bench rpc --method='eth_chainId' --endpoint http://lotus-1:1234/rpc/v1
        ;;
    2)
        echo "Running benchmark test for lotus-1 with method Filecoin.F3GetManifest"
        ./lotus-bench rpc --method='Filecoin.F3GetManifest' --endpoint http://lotus-1:1234/rpc/v1
        ;;
    3)
        echo "Running benchmark test for lotus-2 with method eth_chainId"
        ./lotus-bench rpc --method='eth_chainId' --endpoint http://lotus-2:1235/rpc/v1
        ;;
    4)
        echo "Running benchmark test for lotus-2 with method Filecoin.F3GetManifest"
        ./lotus-bench rpc --method='Filecoin.F3GetManifest' --endpoint http://lotus-2:1235/rpc/v1
        ;;
    *)
        echo "This should never happen"
        ;;
esac