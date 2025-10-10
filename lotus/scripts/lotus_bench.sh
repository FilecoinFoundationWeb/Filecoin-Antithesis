#!/usr/bin/env bash

# GLOBALS

export LOTUS_PATH=${LOTUS_1_PATH}

method_list=(
    Filecoin.ChainHead
    Filecoin.WalletBalance
    Filecoin.ChainGetTipSet
    Filecoin.StateMinerPartitions
    Filecoin.ChainGetParentMessages
    Filecoin.StateLookupID
    Filecoin.ChainGetTipSetByHeight
    Filecoin.StateMinerInfo
    Filecoin.StateReadState
    Filecoin.ChainGetParentReceipts

    eth_feeHistory
    eth_getBlockByNumber

    # METHODS ATTEMPTED BUT TODO
    # eth_getTransactionReceipt
    # eth_getBlockReceipts

    # METHODS NOT YET ADDED
    #eth_getBalance
    #eth_getLogs
    #eth_call
    #eth_getBlockByHash
    #eth_blockNumber
)

endpoint_list=(
    http://"lotus0":1234/rpc/v1
    #http://"lotus1":1234/rpc/v1
)

miner_list=(
    "lotus-miner0"
    "lotus-miner1"
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

echo "Selected method: $method"
echo "Selected endpoint: $endpoint"

# FUNCTIONS

get_random_deadline_index() {
    od -An -N2 -tu2 < /dev/urandom | awk '{ print $1 % 48 }'
}

get_random_epoch() {
    local min_epoch=${1:-0}
    local head_epoch
    head_epoch=$(lotus chain list | tail -n1 | awk '{print $1}' | cut -d':' -f1)

    if [ $? -ne 0 ]; then
        echo "Failed to get epoch head"
        return 1
    fi

    if [ -z "$head_epoch" ] || [ "$head_epoch" -lt "$min_epoch" ]; then
        echo "Invalid chain head epoch or min_epoch > head"
        return 1
    fi

    local range=$(($head_epoch - $min_epoch + 1))
    local random_epoch=$((RANDOM % $range + $min_epoch))

    echo "$random_epoch"
    return 0
}

get_random_block_number() {
    local min_block=${1:-0}

    # Get latest block number in hex via curl
    local latest_block_hex=$(curl -s -X POST "$endpoint" \
      -H "Content-Type: application/json" \
      -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' | jq -r '.result')

    if [ $? -ne 0 ] || [ -z "$latest_block_hex" ]; then
        echo "Failed to get latest block number"
        return 1
    fi

    local latest_block_dec=$((16#${latest_block_hex:2}))

    if [ "$latest_block_dec" -lt "$min_block" ]; then
        echo "Invalid block range"
        return 1
    fi

    local range=$(($latest_block_dec - $min_block + 1))
    local random_block=$((RANDOM % $range + $min_block))

    block_data=$(curl -s -X POST "$endpoint" \
      -H "Content-Type: application/json" \
      -d "{\"jsonrpc\": \"2.0\", \"method\":\"eth_getBlockByNumber\",\"params\":[\"$random_block\", false],\"id\":1}")

    if [ $? -ne 0 ]; then
        echo "Failed to get block data from chosen block"
        return 1
    fi

    block_result=$(echo "$block_data" | jq -r '.result')

    if [ "$block_result" == "null" ]; then
        echo "Block $random_block is null or not available"
        return 1
    fi

    echo "$random_block"
    return 0
}

get_tipsetkey_json() {
    mapfile -t cid_array < <(lotus chain head)

    if [ $? -ne 0 ] || [ ${#cid_array[@]} -eq 0 ]; then
        echo "Failed to get tipset keys"
        return 1
    fi

    local json_cids="["
    for ((i = 0; i < ${#cid_array[@]}; i++)); do
        json_cids+="{\"/\":\"${cid_array[$i]}\"}"
        if [ $i -lt $(( ${#cid_array[@]} - 1 )) ]; then
            json_cids+=","
        fi
    done
    json_cids+="]"

    echo "$json_cids"
    return 0
}

create_wallet_address() {
    local wallet_address
    wallet_address=$(./lotus wallet new bls)

    if [ $? -ne 0 ] || [ -z "$wallet_address" ]; then
        echo "Failed to create wallet address"
        return 1
    fi

    echo "$wallet_address"
    return 0
}

get_cid_json_of_first_block_in_current_tipset() {
    local block_cid
    block_cid=$(lotus chain head | head -n1)

    if [ $? -ne 0 ] || [ -z "$block_cid" ]; then
        echo "Failed to get block CID from chain head"
        return 1
    fi

    local cid_json
    cid_json="{\"/\":\"$block_cid\"}"
    echo "$cid_json"
    return 0
}

get_recent_tx_hash() {
    tx_hash=$(curl -s -X POST "$endpoint" \
        -H "Content-Type: application/json" \
        -d '{
            "jsonrpc": "2.0",
            "method": "eth_getBlockByNumber",
            "params": ["latest", true],
            "id": 1
        }' | jq -r '.result.hash'
    )

    if [ $? -ne 0 ]; then
        echo "Curl command failed"
        return 1
    fi

    if [ "$tx_hash" == "null" ] || [ -z "$tx_hash" ]; then
        echo "No transaction hash found in latest block"
        return 1
    fi

    echo "$tx_hash"
    return 0
}

# CASES

case $method in

    Filecoin.ChainHead)
        ./lotus-bench rpc --method="$method" --endpoint="$endpoint"
        ;;
    Filecoin.WalletBalance)
        wallet_address=$(create_wallet_address) || exit 0
        echo "wallet address: $wallet_address"

        ./lotus-bench rpc --method="$method:::[\"$wallet_address\"]" --endpoint="$endpoint"
        ;;
    Filecoin.ChainGetTipSet)
        tipset_json=$(get_tipsetkey_json) || exit 0
        echo "tipset_json: $tipset_json"
        
        ./lotus-bench rpc --method="$method:::[$tipset_json]" --endpoint="$endpoint"
        ;;
    Filecoin.StateMinerPartitions)
        miner_index=$(get_random_index ${#miner_list[@]})
        miner_address=${miner_list[$miner_index]}
        echo "miner address: $miner_address"

        deadline_index=$(get_random_deadline_index)
        echo "random deadline index: $deadline_index"

        tipset_json=$(get_tipsetkey_json) || exit 0
        echo "tipset_json: $tipset_json"

        ./lotus-bench rpc --method="$method:::[\"$miner_address\", $deadline_index, $tipset_json]" --endpoint="$endpoint"
        ;;
    Filecoin.ChainGetParentMessages)
        cid_json=$(get_cid_json_of_first_block_in_current_tipset) || exit 0
        echo "cid: $cid_json"

        ./lotus-bench rpc --method="$method:::[$cid_json]" --endpoint="$endpoint"
        ;;
    Filecoin.StateLookupID)
        miner_index=$(get_random_index ${#miner_list[@]})
        miner_address=${miner_list[$miner_index]}
        echo "miner address: $miner_address"

        tipset_json=$(get_tipsetkey_json) || exit 0
        echo "tipset_json: $tipset_json"

        ./lotus-bench rpc --method="$method:::[\"$miner_address\", $tipset_json]" --endpoint="$endpoint"
        ;;
    Filecoin.ChainGetTipSetByHeight)
        epoch=$(get_random_epoch 10) || exit 0
        echo "random epoch: $epoch"

        tipset_json=$(get_tipsetkey_json) || exit 0
        echo "tipset_json: $tipset_json"

        ./lotus-bench rpc --method="$method:::[$epoch, $tipset_json]" --endpoint="$endpoint"
        ;;
    Filecoin.StateMinerInfo)
        miner_index=$(get_random_index ${#miner_list[@]})
        miner_address=${miner_list[$miner_index]}
        echo "miner address: $miner_address"

        tipset_json=$(get_tipsetkey_json) || exit 0
        echo "tipset_json: $tipset_json"

        ./lotus-bench rpc --method="$method:::[\"$miner_address\", $tipset_json]" --endpoint="$endpoint"
        ;;
    Filecoin.StateReadState)
        miner_index=$(get_random_index ${#miner_list[@]})
        miner_address=${miner_list[$miner_index]}
        echo "miner address: $miner_address"

        tipset_json=$(get_tipsetkey_json) || exit 0
        echo "tipset_json: $tipset_json"

        ./lotus-bench rpc --method="$method:::[\"$miner_address\", $tipset_json]" --endpoint="$endpoint"
        ;;
    Filecoin.ChainGetParentReceipts)
        cid_json=$(get_cid_json_of_first_block_in_current_tipset) || exit 0
        echo "cid: $cid_json"

        ./lotus-bench rpc --method="$method:::[$cid_json]" --endpoint="$endpoint"
        ;;
    eth_feeHistory)
        blocks_to_look_at=$(printf "0x%x" 10)
        newest_block="latest"
        reward_percentiles="[5, 25, 50, 75, 95]"

        ./lotus-bench rpc --method="$method:::[\"$blocks_to_look_at\", \"$newest_block\", $reward_percentiles]" --endpoint="$endpoint"
        ;;
    eth_getTransactionByHash)
        tx_hash=$(get_recent_tx_hash) || exit 0
        echo "transaction hash: $tx_hash"
        # ./lotus-bench rpc --method="$method:::[\"$tx_hash\"]" --endpoint="$endpoint"
        ;;
    eth_getBlockByNumber)
        block_number=$(get_random_block_number) || exit 0
        echo "random block number: $block_number"
        full_tx_objects="false"
        echo "full transaction objects: $full_tx_objects"
        ./lotus-bench rpc --method="$method:::[\"$block_number\", $full_tx_objects]" --endpoint="$endpoint"
        ;;
    eth_getTransactionReceipt)
        tx_hash=$(get_recent_tx_hash) || exit 0
        echo "transaction hash: $tx_hash"
        ./lotus-bench rpc --method="$method:::[$tx_hash]" --endpoint="$endpoint"
        ;;
esac