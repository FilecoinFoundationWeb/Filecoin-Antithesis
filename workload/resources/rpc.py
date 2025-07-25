#!/usr/bin/env -S python3 -u

from request import request
import json

from antithesis.assertions import (
    always,
    sometimes
)


def get_genesis_wallet(node_type:str, rpc_url:str, auth_token:str) -> str:
    '''
    @purpose - get genesis wallet
    @param rpc_url - endpoint address for node
    @param auth_token - authentication token for that node
    @return - genesis wallet id hash as a str
    '''
    method = "Filecoin.WalletList"
    payload = json.dumps({
        "jsonrpc": "2.0",
        "id": "1",
        "method": method
    })
    response = request(node_type, rpc_url, auth_token, "post", payload)
    if response is None or response['response'].status_code != 200:
        sometimes(False, "List existing wallets on the chain", {"node_type":node_type,"response":response})
        print(f"Workload [rpc.py]: bad response status code during get_genesis_wallet for {method} on a {node_type} node")
        return None
    sometimes(True, "List existing wallets on the chain", None)
    print(f"Workload [rpc.py]: good response status code during get_genesis_wallet for {method} on a {node_type} node")
    response_body = response['response'].json()

    # questionable way to pick out genesis wallet. its hash is much longer than a regular wallet...
    for w in response_body['result']:
        if len(w) > 41:
            sometimes(True, "Found the genesis wallet", None)
            print("Workload [rpc.py]: found the genesis wallet. returning its hash")
            return w
    sometimes(False, "Found the genesis wallet", {"List of wallets":response_body['result']})
    print("Workload [rpc.py]: failed to find genesis wallet")
    return None


def create_wallet(node_type:str, rpc_url:str, auth_token:str) -> str:
    '''
    @purpose - create a wallet
    @param rpc_url - endpoint address for node
    @param auth_token - authentication token for that node
    @return - new wallet id hash as a str
    '''
    method = 'Filecoin.WalletNew'
    payload = json.dumps({
        "jsonrpc": "2.0",
        "id": "1",
        "method": method,
        "params": [1]
    })
    response = request(node_type, rpc_url, auth_token, 'post', payload)
    if response is None or response['response'].status_code != 200:
        sometimes(False, "Create a new wallet", {"node_type":node_type,"response":response})
        print(f"Workload [rpc.py]: bad response status code during create_wallet for {method} on a {node_type} node")
        return None
    sometimes(True, "Create a new wallet", None)
    print(f"Workload [rpc.py]: good response status code during create_wallet for {method} on a {node_type} node")
    return response['response'].json()['result']


def delete_wallet(node_type:str, rpc_url:str, auth_token:str, wallet:str) -> bool:
    '''
    @purpose - delete a wallet
    @param rpc_url - endpoint address for node
    @param auth_token - authentication token for that node
    @param wallet - wallet hash id for wallet that we want to delete
    '''
    method = 'Filecoin.WalletDelete'
    payload = json.dumps({
        "jsonrpc": "2.0",
        "id": "1",
        "method": method,
        "params": [wallet]
    })
    response = request(node_type, rpc_url, auth_token, 'post', payload)
    if response is None or response['response'].status_code != 200:
        sometimes(False, "Delete a wallet", {"node type":node_type,"response":response})
        print(f"Workload [rpc.py]: bad response status code during delete_wallet for {method} on a {node_type} node")
        return False
    sometimes(True, "Delete a wallet", None)
    print(f"Workload [rpc.py]: good response status code during delete_wallet for {method} on a {node_type} node")
    return True


def get_wallet_private_key(node_type:str, rpc_url:str, auth_token:str, wallet:str) -> str:
    '''
    @purpose - get the private key of any existing wallet
    @param rpc_url - endpoint address for node
    @param auth_token - authentication token for that node
    @param wallet - a wallet id hash
    @return - private key of the wallet
    '''
    method = 'Filecoin.WalletExport'
    payload = json.dumps({
        "jsonrpc": "2.0",
        "id": "1",
        "method": method,
        "params": [wallet]
    })
    response = request(node_type, rpc_url, auth_token, 'post', payload)
    if response is None or response['response'].status_code != 200:
        sometimes(False, "Get a wallet private key", {"node type":node_type,"response":response})
        print(f"Workload [rpc.py]: bad response status code during get_wallet_private_key for {method} on a {node_type} node")
        return None
    sometimes(True, "Get a wallet private key", None)
    print(f"Workload [rpc.py]: good response status code during get_wallet_private_key for {method} on a {node_type} node")
    return response['response'].json()['result']['PrivateKey']


def get_wallet_balance(node_type:str, rpc_url:str, auth_token: str, wallet:str):
    '''
    @purpose - get the amount of FIL in a wallet
    '''
    method = 'Filecoin.WalletBalance'
    payload = json.dumps({
        "jsonrpc": "2.0",
        "id": "1",
        "method": method,
        "params": [wallet]
    })
    response = request(node_type, rpc_url, auth_token, 'post', payload)
    if response is None or response['response'].status_code != 200:
        # sdk
        print(f"Workload [rpc.py]: bad response status code during get_wallet_balance for {method} on a {node_type} node")
        return None
    # sdk
    print(f"Workload [rpc.py]: good response status code during get_wallet_balance for {method} on a {node_type} node")
    print("debugging!!!!!!")
    print(response)
    print("---------------")
    print(response['response'])
    print("END OF DEBUGGING!!!!!")


def get_chainhead(node_type, rpc_url:str, auth_token:str) -> str:
    '''
    *** This method doesn't need auth_token ?
    @purpose - get the chainhead
    @param rpc_url - endpoint address for node
    @param auth_token - authentication token for that node
    @return - chainhead id that we can use to push messages
    '''
    method = 'Filecoin.ChainHead'
    payload = json.dumps({
        "jsonrpc": "2.0",
        "id": "1",
        "method": method
    })
    response = request(node_type, rpc_url, auth_token, 'post', payload)
    if response is None or response['response'].status_code != 200:
        sometimes(False, "Get the chainhead", {"node type":node_type,"response":response})
        print(f"Workload [rpc.py]: bad response status code during get_chainhead for {method} on a {node_type} node")
        return None
    sometimes(True, "Get the chainhead", None)
    print(f"Workload [rpc.py]: good response status code during get_chainhead for {method} on a {node_type} node")
    response_body = response['response'].json()

    cid = ''
    if len(response_body['result']['Cids']) > 0:
        cid = response_body['result']['Cids'][0]
        if '/' in cid:
            return response_body
    return None


def estimate_message_gas(node_type:str, rpc_url:str, auth_token:str, from_wallet:str, from_wallet_pk:str, to_wallet:str, fil:str) -> dict:
    '''
    @purpose - estimate gas for a mpool message
    @param rpc_url - endpoint address for node
    @param auth_token - authentication token for that node
    @param from_wallet - wallet id hash that will be giving FIL
    @param from_wallet_pk - wallet private key that will be giving FIL
    @param to_wallet - wallet id hash that will be receiving FIL
    @param fil - fil of FIL to be transacted
    @return - dictionary with Nonce, Value, GasLimit, GasFeeCap, GasPremium
    '''
    method = 'Filecoin.GasEstimateMessageGas'
    payload = json.dumps({
        "jsonrpc": "2.0",
        "id": "1",
        "method": method,
        "params": [
            {
                # "Version": 42,
                "To": to_wallet,
                "From": from_wallet,
                "Value": fil,
                "Method": 0,
                "Params": from_wallet_pk,
                "GasLimit": 10000000
            },
            {
                "MaxFee": "0"
            }, None
        ]
    })
    response = request(node_type, rpc_url, auth_token, 'post', payload)
    if response is None or response['response'].status_code != 200:
        sometimes(False, "Estimate message gas for a transaction", {"node type":node_type,"response":response})
        print(f"Workload [rpc.py]: bad response status code during estimate_message_gas for {method} on a {node_type} node")
        return None
    sometimes(True, "Estimate message gas for a transaction", None)
    print(f"Workload [rpc.py]: good response status code during estimate_message_gas for {method} on a {node_type} node")
    return response['response'].json()['result']


def mpool_push_message(node_type:str, rpc_url:str, auth_token:str, from_wallet:str, from_wallet_pk:str, to_wallet:str, fil:str, gas_info:dict, cid:str):
    '''
    @purpose - push a transaction message
    @param rpc_url - endpoint address for node
    @param auth_token - authentication token for that node
    @param from_wallet - wallet id hash that will be giving FIL
    @param from_wallet_pk - wallet private key that will be giving FIL
    @param to_wallet - wallet id hash that will be receiving FIL
    @param fil - fil of FIL to be transacted
    @param gas_info - estimate gas info for the mpool message
    @param cid - chainhead id
    @return - request response. -- maybe should change this
    '''
    method = 'Filecoin.MpoolPushMessage'
    payload = json.dumps({
        "jsonrpc": "2.0",
        "id": "1",
        "method": method,
        "params": [
            {
                # "Version": 42,
                "To": to_wallet,
                "From": from_wallet,
                "Value": fil,
                # "GasLimit": gas_info['GasLimit'],
                # "GasFeeCap": gas_info['GasFeeCap'],
                # "GasPremium": gas_info['GasPremium'],
                "GasLimit": int(float(gas_info['GasLimit'])),
                "GasFeeCap": str(int(float(gas_info['GasFeeCap']))),
                "GasPremium": str(int(float(gas_info['GasPremium']))),
                "Method": 0,
                "Params": from_wallet_pk,
                # "Params": "",
                "CID": {
                "/": cid
                }
            },
            {
                "MaxFee": "0"
            }
        ]
    })
    response = request(node_type, rpc_url, auth_token, 'post', payload)
    if response is None or response['response'].status_code != 200:
        sometimes(False, "Push message to mpool", {"node type":node_type,"response":response})
        print(f"Workload [rpc.py]: bad response status code during push_message for {method} on a {node_type} node")
        return None
    sometimes(True, "Push message to mpool", None)
    print(f"Workload [rpc.py]: good response status code during push_message for {method} on a {node_type} node")
    return response


def sync_state(node_type:str, rpc_url:str, auth_token:str):
    '''
    @purpose - get the sync status of a node
    @param rpc_url - endpoint address for a node
    @param auth_token - authentication token for that node
    @return - list of active syncs. each element is a dictionary with WorkerID, Base, Target, Stage, Height, Start, End, and Message keys
    '''
    method = 'Filecoin.SyncState'
    payload = json.dumps({
        "jsonrpc": "2.0",
        "id": "1",
        "method": method,
        "params": []
    })
    response = request(node_type, rpc_url, auth_token, "post", payload)
    if response is None or response['response'].status_code != 200:
        #sdk.always(declare=False, id="Get status of a sync state", message="Get status of a sync state", condition=False, details={"node type":node_type,"response":response['response']})
        print(f"Workload [rpc.py]: bad response status code during SyncState for {method} on a {node_type} node")
        return None
    #sdk.always(declare=False, id="Get status of a sync state", message="Get status of a sync state", condition=True)
    print(f"Workload [rpc.py]: good response status code during SyncState for {method} on a {node_type} node")
    return response['response'].json()['result']['ActiveSyncs']
