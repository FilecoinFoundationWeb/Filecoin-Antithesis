#!/usr/bin/env bash

method_list=(
    eth_call
    Filecoin.StateMinerInfo
    Filecoin.ChainHead
    eth_getBalance
    eth_getBlockByNumber
    eth_blockNumber
    eth_getLogs
    eth_getTransactionReceipt
    eth_getBlockByHash
    Filecoin.ChainGetTipSetByHeight
    Filecoin.WalletBalance
    Filecoin.StateMinerPartitions
    Filecoin.StateReadState
    eth_getTransactionByHash
    eth_getBlockReceipts
    Filecoin.StateLookupID
    eth_feeHistory
    Filecoin.ChainGetParentReceipts
    Filecoin.ChainGetParentMessages
    #Filecoin.ChainGetTipSet
)

endpoint_list=(
    http://lotus-1:1234/rpc/v1
    http://lotus-2:1235/rpc/v1
)

get_random_index() {
    local array_length=$1
    local rand
    rand=$(od -An -N2 -tu2 /dev/urandom)
    rand=${rand//[[:space:]]/}
    echo $((rand % array_length))
}

method_index=$(get_random_index ${#method_list[@]})
method=${method_list[$method_index]}

endpoint_index=$(get_random_index ${#endpoint_list[@]})
endpoint=${endpoint_list[$endpoint_index]}

echo "Running benchmark test for $endpoint for method $method"

./lotus-bench rpc --method="$method" --endpoint="$endpoint"