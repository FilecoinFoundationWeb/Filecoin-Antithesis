#!/usr/bin/env -S python3 -u

import rpc, time

from antithesis.assertions import (
    always,
    reachable,
    unreachable,
)

        
def make_transaction(node_type:str, rpc_url:str, auth_token:str, from_wallet:str, from_wallet_pk:str, to_wallet:str, attoFIL:int) -> bool: 
    '''
    @purpose - make a transaction from a wallet 
    @param rpc_url - endpoint address for node
    @param auth_token - authentication token for that node
    @param from_wallet - wallet id hash that will be giving FIL
    @param from_wallet_pk - wallet private key that will be giving FIL
    @param to_wallet - wallet id hash that will be receiving FIL
    @param attoFIL - amount of attoFIL
    @return bool if transaction went through
    '''
    fil_amount = str(attoFIL*10**18)
    chainhead = rpc.get_chainhead(node_type=node_type, rpc_url=rpc_url, auth_token=auth_token)

    if (not bool(chainhead)):
        print(f"Workload [transaction.py]: failed to get CID from get_chainhead RPC call on a {node_type} node")
        return False
    
    cid = chainhead['result']['Cids'][0]['/']
    
    gas_info = rpc.estimate_message_gas(node_type=node_type, rpc_url=rpc_url, auth_token=auth_token, from_wallet=from_wallet, from_wallet_pk=from_wallet_pk, to_wallet=to_wallet, fil=fil_amount)
    if (not bool(gas_info)):
        print(f'Workload [transaction.py]: failed to get gas information from estimate_message_gas RPC call on a {node_type} node')
        return False
    
    # print(f"Workload [transaction.py]: GasLimit: {gas_info['GasLimit']}, GasFeeCap: {gas_info['GasFeeCap']}, GasPremium: {gas_info['GasPremium']}")

    txn_response = rpc.mpool_push_message(node_type=node_type, rpc_url=rpc_url, auth_token=auth_token, from_wallet=from_wallet, from_wallet_pk=from_wallet_pk, to_wallet=to_wallet, fil=fil_amount, gas_info=gas_info, cid=cid)
    if txn_response:
        print(f"Workload [transaction.py]: a successful transaction on a {node_type} node")
        always(True, "Executed a transaction", None)
        return True
    print(f"Workload [transaction.py]: a failed transaction on a {node_type} node")
    always(False, "Executed a transaction", {"node_type":node_type,"response":txn_response})
    return False


def feed_wallets(node_type:str, rpc_url:str, auth_token:str, genesis_wallet:str, genesis_wallet_pk:str, to_wallets:list, attoFIL:int):
    '''
    @purpose - give a list of wallets FIL from the genesis wallet
    @param rpc_url - endpoint address for node
    @param auth_token - authentication token for that node
    @param genesis_wallet - genesis wallet hash id. will give to_wallets FIL
    @param genesis_wallet_pk - genesis wallet private key
    @param to_wallets - list of wallets to give FIL
    @param v - amount of FIL in attoFIL units
    '''
    num_wallets = len(to_wallets)
    wallets_fed, backoff = 0, 0
    print(f"Workload [transaction.py]: attempting to give FIL to {num_wallets} wallets from the genesis wallet")
    while wallets_fed < num_wallets:
        if backoff >= 16:
            unreachable("Timeout: give wallets FIL from genesis wallet", None)
            print(f"Workload [transaction.py]: failed to give wallet FIL after a long time on a {node_type} node. this is a serious issue. only finished {num_wallets} wallets. finishing early.")
            return
        succeed = make_transaction(node_type=node_type, rpc_url=rpc_url, auth_token=auth_token, from_wallet=genesis_wallet, from_wallet_pk=genesis_wallet_pk, to_wallet=to_wallets[wallets_fed], attoFIL=attoFIL)
        if succeed:
            wallets_fed += 1
            print(f"Workload [transaction.py]: gave wallet #{wallets_fed} {attoFIL} attoFIL. progress: {wallets_fed} / {num_wallets}")
            backoff = 0
        else:
            backoff += 1
            print(f"Workload [transaction.py]: failed to give FIL to wallet. retrying... attempt {backoff+1} for wallet #{wallets_fed+1}")
            time.sleep(backoff)
    reachable("Give wallets FIL from the genesis wallet", None)
    print(f"Workload [transaction.py]: successfully gave FIL to wallets from the genesis wallet")
