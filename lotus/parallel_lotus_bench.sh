#!/usr/bin/env bash

method_list=(
    eth_feeHistory
    Filecoin.ChainHead

    # THE FOLLOWING EXIT WITH 500 STATUS CODES (NEED MORE PARAMETERS)
    #Filecoin.StateMinerPartitions
    #Filecoin.ChainGetParentMessages
    #Filecoin.WalletBalance
    #eth_getTransactionByHash
    #Filecoin.StateLookupID
    #eth_getBlockByNumber
    #eth_getTransactionReceipt
    #eth_getBlockReceipts
    #eth_getBalance
    #Filecoin.ChainGetTipSetByHeight
    #Filecoin.StateMinerInfo
    #Filecoin.ChainGetTipSet
    #Filecoin.StateReadState
    #eth_getLogs
    #eth_call
    #eth_getBlockByHash
    #Filecoin.ChainGetParentReceipts
    #eth_blockNumber
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