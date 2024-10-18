import requests
import json
import os
import time
import random

class FilSpammerRPC:
    def __init__(self, rpc_url, token_path):
        self.rpc_url = rpc_url
        self.auth_token = self._read_auth_token(token_path)

    def _read_auth_token(self, token_path):
        with open(token_path) as f:
            auth_token = f.read().strip()
        return auth_token

    def do_request(self, method, payload):
        headers = {
            "Content-Type": "application/json",
            "Authorization": f'Bearer {self.auth_token}'
        }
        response = requests.post(self.rpc_url, headers=headers, data=json.dumps(payload))
        return response.json()

    def create_wallet(self):
        method = 'Filecoin.WalletNew'
        payload = {
            "jsonrpc": "2.0",
            "id": "1",
            "method": method,
            "params": [1]
        }
        res = self.do_request('post', payload)
        if 'error' in res:
            print(f'Error in {method}:', res['error'])
            return None
        return res['result']

    def get_chainhead(self):
        method = 'Filecoin.ChainHead'
        payload = {
            "jsonrpc": "2.0",
            "id": "1",
            "method": method
        }
        res = self.do_request('post', payload)
        if 'error' in res:
            print(f'Error in {method}:', res['error'])
            return None
        return res['result']['Cids'][0]['/']

    def _estimate_message_gas(self, wallet_from_id, wallet_to_id, amount):
        method = 'Filecoin.GasEstimateMessageGas'
        payload = {
            "jsonrpc": "2.0",
            "id": "1",
            "method": method,
            "params": [
                {
                    "To": wallet_to_id,
                    "From": wallet_from_id,
                    "Value": amount,
                    "GasLimit": 10000000,
                    "Method": 0,
                },
                {
                    "MaxFee": "0"
                }, None
            ]
        }
        res = self.do_request('post', payload)
        if 'error' in res:
            print(f'Error in {method}:', res['error'])
            return None
        return res['result']

    def push_message(self, wallet_from_id, wallet_to_id, amount, nonce):
        cid = self.get_chainhead()
        if not cid:
            print('Failed to get CID from chainhead RPC call')
            return None

        gas_info = self._estimate_message_gas(wallet_from_id, wallet_to_id, amount)
        if not gas_info:
            print('Failed to get gas information to do MpoolPushMessage')
            return None

        method = 'Filecoin.MpoolPushMessage'
        payload = {
            "jsonrpc": "2.0",
            "id": "1",
            "method": method,
            "params": [
                {
                    "Version": 0,
                    "To": wallet_to_id,
                    "From": wallet_from_id,
                    "Nonce": nonce,
                    "Value": amount,
                    "GasLimit": gas_info['GasLimit'],
                    "GasFeeCap": gas_info['GasFeeCap'],
                    "GasPremium": gas_info['GasPremium'],
                    "Method": 0,
                    "Params": "",
                    "CID": {
                        "/": cid
                    }
                },
                {
                    "MaxFee": "0"
                }
            ]
        }
        res = self.do_request('post', payload)
        if 'error' in res:
            print(f'Error in {method}:', res['error'])
            return None
        return res

    def get_genesis_wallet(self):
        genesis_wallet = False
        method = 'Filecoin.WalletList'
        payload = {
            "jsonrpc": "2.0",
            "id": "1",
            "method": method,
            "params": []
        }
        res = self.do_request('post', payload)
        if 'error' in res:
            print(f'Error in {method}:', res['error'])
            return False

        example_non_genesis_wallet = "t1sua7sz4h43j3rhsh3x7f5ciysyck7bqzme3dtxy"
        response_body = res
        for wallet in response_body['result']:
            if len(wallet) > len(example_non_genesis_wallet):
                genesis_wallet = wallet

        return genesis_wallet


def create_wallets(spammer, num_wallets):
    wallets = []
    for _ in range(num_wallets):
        wallet_id = spammer.create_wallet()
        if wallet_id:
            wallets.append(wallet_id)
    return wallets

def transfer_funds(spammer, genesis_wallet, wallets):
    for wallet_id in wallets:
        res = spammer.push_message(genesis_wallet, wallet_id, '100', 0)
        if res:
            print(f'Transferred 100 FIL from {genesis_wallet} to {wallet_id}')
        else:
            print(f'Failed to transfer FIL to {wallet_id}')

def transfer_between_wallets(spammer, wallets):
    with open('/dev/urandom', 'rb') as f:
        random_bytes = f.read(1)
        seed = int.from_bytes(random_bytes, byteorder='big')
        print(f'Getting a random SEED {seed}')
        random.seed(seed)

    for _ in range(10):  # Performing 10 random transfers
        from_wallet = random.choice(wallets)
        to_wallet = random.choice(wallets)
        while to_wallet == from_wallet:
            to_wallet = random.choice(wallets)
        res = spammer.push_message(from_wallet, to_wallet, '1', 0)
        if res:
            print(f'Transferred 1 FIL from {from_wallet} to {to_wallet}')
        else:
            print(f'Failed to transfer FIL from {from_wallet} to {to_wallet}')

if __name__ == '__main__':
    BASE_PATH1 = '/root/devgen'
    BASE_PATH2 = '/root/devgen'
    TOKEN_LOTUS_1 = 'lotus-1-token.txt'
    TOKEN_LOTUS_2 = 'lotus-2-token.txt'

    parser = argparse.ArgumentParser()

    parser.add_argument(
        "step",
        type=str,
        help="the workload step to run",
        choices=[
            "1_create_wallets",
            "2_transfer_funds",
            "3_transfer_between_wallets"
        ],
        default="1_create_wallets",
    )

    args = parser.parse_args()

    lotus1_rpc = os.getenv("RPC_LOTUS1")
    lotus2_rpc = os.getenv("RPC_LOTUS2")

    if not lotus1_rpc or not lotus2_rpc:
        print('Workload cannot start, missing environment variable RPC_LOTUS1 or RPC_LOTUS2')
        exit(2)

    lotus1_token = FilSpammerRPC._read_auth_token(os.path.expanduser(f'{BASE_PATH1}/{TOKEN_LOTUS_1}'))
    lotus2_token = FilSpammerRPC._read_auth_token(os.path.expanduser(f'{BASE_PATH2}/{TOKEN_LOTUS_2}'))

    if not lotus1_token or not lotus2_token:
        print('Workload cannot start, unable to fetch auth tokens from Lotus nodes, make sure they are generated')
        exit(3)

    spammer1 = FilSpammerRPC(lotus1_rpc, lotus1_token)
    spammer2 = FilSpammerRPC(lotus2_rpc, lotus2_token)

    if args.step == '1_create_wallets':
        print('Executing step 1, creating wallets on lotus nodes')
        wallets1 = create_wallets(spammer1, 10)
        wallets2 = create_wallets(spammer2, 10)
        with open(f'{BASE_PATH1}/wallets1.json', 'w') as f:
            json.dump(wallets1, f)
        with open(f'{BASE_PATH2}/wallets2.json', 'w') as f:
            json.dump(wallets2, f)
        print(f'Created wallets for {lotus1_rpc}: {wallets1}')
        print(f'Created wallets for {lotus2_rpc}: {wallets2}')
    elif args.step == '2_transfer_funds':
        print('Executing step 2, transferring funds on lotus nodes')
        genesis_wallet1 = spammer1.get_genesis_wallet()
        genesis_wallet2 = spammer2.get_genesis_wallet()
        if not genesis_wallet1 or not genesis_wallet2:
            print('Unable to retrieve genesis wallet')
            exit(4)
        print(f'Genesis wallet for {lotus1_rpc}: {genesis_wallet1}')
        print(f'Genesis wallet for {lotus2_rpc}: {genesis_wallet2}')
        with open(f'{BASE_PATH1}/wallets1.json', 'r') as f:
            wallets1 = json.load(f)
        with open(f'{BASE_PATH2}/wallets2.json', 'r') as f:
            wallets2 = json.load(f)
        transfer_funds(spammer1, genesis_wallet1, wallets1)
        transfer_funds(spammer2, genesis_wallet2, wallets2)
        print(f'Transferred funds for {lotus1_rpc}')
        print(f'Transferred funds for {lotus2_rpc}')
    elif args.step == '3_transfer_between_wallets':
        print('Executing step 3, transferring funds between wallets on lotus nodes')
        with open(f'{BASE_PATH1}/wallets1.json', 'r') as f:
            wallets1 = json.load(f)
        with open(f'{BASE_PATH2}/wallets2.json', 'r') as f:
            wallets2 = json.load(f)
        transfer_between_wallets(spammer1, wallets1)
        transfer_between_wallets(spammer2, wallets2)
        print(f'Transferred funds between wallets for {lotus1_rpc}')
        print(f'Transferred funds between wallets for {lotus2_rpc}')
