'''
@todo: 
1. Adding leader/follower entrypoint to generate a RPC token and place it in a common volume mount (done)
2. Create argparser to pass in the RPC endpoint and read the token from common volume mount and pass it in from the entrypoint
3. Create an entrypoint for the workload to wait for the token to be created (which means start up more or less completed before it starts to do things) (in-progress)
4. Message in the workload that actually start the fault
'''

import requests, json, random, argparse, os
# import json

class fil_spammer_rpc():
    def __init__(self, rpc_url:str, auth_token:str, common_mount_path:str):
        self.auth_token = auth_token
        self.rpc_url = rpc_url
        self.basepath = common_mount_path

    def do_request(self, method:str, payload:dict) -> dict:
        headers = {
            "Content-Type": "application/json",
            "Authorization": f'Bearer {self.auth_token}'
        }

        if method in ['get', 'post', 'put', 'delete', 'head', 'options']:
            payload_mapping = {
                'get': 'params',
                'post': 'data',
            }
            kwargs = {}
            if bool(payload):
                if method in payload_mapping.keys():
                    kwargs.update({payload_mapping[method]: payload})

            func = getattr(requests, method)
            response = func(self.rpc_url, headers=headers, **kwargs)
            response_json = response.json()

            return {
                'request': {
                    'url': self.rpc_url,
                    'headers': headers,
                    'payload': payload,
                },
                'response': response,
                'response_json': response_json
            }

    def create_wallet(self) -> str:
        method = 'Filecoin.WalletNew'
        payload = json.dumps({
            "jsonrpc": "2.0",
            "id": "1",
            "method": method,
            "params": [1]
        })
        res = self.do_request('post', payload)

        if res['response'].status_code != 200:
            print(f'Bad response from {method}')
            print(res['response_json'])
            return False

        return res['response_json']['result']

    def get_chainhead(self):
        method = 'Filecoin.ChainHead'
        payload = json.dumps({
            "jsonrpc": "2.0",
            "id": "1",
            "method": method
        })
        res = self.do_request('post', payload)

        if res['response'].status_code != 200:
            print(f'Bad response from {method}')
            print(res['response_json'])
            return False

        response_body = res['response_json']
        if len(response_body['result']['Cids']) > 0:
            cid = response_body['result']['Cids'][0]
            if '/' in cid:
                cid = cid['/']
            return cid
        return None

    def get_genesis_wallet(self):
        genesis_wallet = False
        method = 'Filecoin.WalletList'
        payload = json.dumps({
            "jsonrpc": "2.0",
            "id": "1",
            "method": method
        })
        res = self.do_request('post', payload)

        if res['response'].status_code != 200:
            print(f'Bad response from {method}')
            print(res['response_json'])
            return False

        example_non_genesis_wallet = "t1sua7sz4h43j3rhsh3x7f5ciysyck7bqzme3dtxy"
        response_body = res['response_json']
        for wallet in response_body['result']:
            if len(wallet) > len(example_non_genesis_wallet):
                genesis_wallet = wallet

        return genesis_wallet

    def transfer_from_genesis(self, genesis_wallet, destination_wallet_id:str, amount:str):
        command = [
            "./lotus-local-net/lotus",
            "send",
            "--from",
            genesis_wallet,
            destination_wallet_id,
            amount
        ]
        result = subprocess.run(command, capture_output=True, text=True)
        return result

    def _estimate_message_gas(self, wallet_from_id:str, wallet_to_id:str, amount:str) -> dict:
        method = 'Filecoin.GasEstimateMessageGas'
        payload = json.dumps({
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
        })

        res = self.do_request('post', payload)

        if res['response'].status_code != 200:
            print(f'Bad response from {method}')
            print(res['response_json'])
            return False

        return res['response_json']['result']

    def push_message(self, wallet_from_id:str, wallet_to_id: str, amount:str, nonce:int):
        cid = self.get_chainhead()
        if not bool(cid):
            print('Failed to get CID from chainhead RPC call')
            return

        gas_info = self._estimate_message_gas(wallet_from_id, wallet_to_id, amount)
        if not bool(gas_info):
            print('Failed to get gas information to do MpoolPushMessage')
            return

        method = 'Filecoin.MpoolPushMessage'
        payload = json.dumps({
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
        })
        res = self.do_request('post', payload)

        if res['response'].status_code != 200:
            print(f'Bad response from {method}')
            print(res['response_json'])
            return False
        return res

    def fuzz_push_message(self, wallet_from_id:str, wallet_to_id: str, amount:str, nonce:int):
        # Define possible fuzzing scenarios
        fuzz_cases = [
            {"Nonce": -1},  # Invalid nonce
            {"Value": "0"},  # Zero amount
            {"Value": "-100"},  # Negative amount
            {"Value": "1000000000000000000000"},  # Excessively high amount
            {"GasLimit": "0"},  # Zero gas limit
            {"GasLimit": "-1000"},  # Negative gas limit
            {"To": "t1invalidwallet"},  # Invalid recipient address
            {"GasFeeCap": "0"},  # Zero gas price
            {"GasFeeCap": "-1000"},  # Negative gas price
        ]

        for fault in fuzz_cases:  # Test each fault separately
            cid = self.get_chainhead()
            if not bool(cid):
                print('Failed to get CID from chainhead RPC call')
                continue

            gas_info = self._estimate_message_gas(wallet_from_id, wallet_to_id, amount)
            if not bool(gas_info):
                print('Failed to get gas information to do MpoolPushMessage')
                continue

            method = 'Filecoin.MpoolPushMessage'
            fuzz_payload = {
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
                "CID": {"/": cid}
            }

            # Inject the fault
            fuzz_payload.update(fault)

            payload = json.dumps({
                "jsonrpc": "2.0",
                "id": "1",
                "method": method,
                "params": [fuzz_payload, {"MaxFee": "0"}]
            })

            res = self.do_request('post', payload)
            print(f'Fuzzing with fault {fault}: response', res['response_json'])


def get_lotus_rpc_auth_token(token_path:str) -> str:
    '''
    Quick helper to get the auth token
    '''
    auth_token = ''
    with open(token_path) as f:
        auth_token = f.read()

    # Create wallets
    wallets = []
    for _ in range(num_wallets):
        wallet_id = spammer.create_wallet()
        wallets.append(wallet_id)

    return auth_token

if __name__ == '__main__':

    TOKEN_LOTUS_1 = 'lotus-1-token.txt'
    TOKEN_LOTUS_2 = 'lotus-2-token.txt'
    BASE_PATH = '/root/devgen'

    parser = argparse.ArgumentParser()

    # The default argument
    parser.add_argument(
        "step",
        type=str,
        help="the workload step to run",
        choices=[
            "1_create_wallets",
            "2_transfer_funds", # Transfer funds from genesis
            "3_spam_transactions",
        ],
        default="1_create_wallets",
    )

    args = parser.parse_args()

    # Initial sanity checks
    lotus1_rpc = os.getenv("RPC_LOTUS1")
    lotus2_rpc = os.getenv("RPC_LOTUS2")
    
    if not bool(lotus1_rpc) or not bool(lotus2_rpc):
        print('Workload cannot start, missing environment variable RPC_LOTUS1 or RPC_LOTUS2')
        exit(2)

    lotus1_token = get_lotus_rpc_auth_token(f'{BASE_PATH}/{TOKEN_LOTUS_1}')
    lotus2_token = get_lotus_rpc_auth_token(f'{BASE_PATH}/{TOKEN_LOTUS_2}')

    if not bool(lotus1_token) or not bool(lotus2_token):
        print('Workload cannot start, unable to fetch auth tokens from Lotus nodes, make sure they are generated')
        exit(3)

    # We are ready for business
    print('Lotus RPC information:')
    print(f'Lotus 1: {lotus1_rpc}')
    print(f'Lotus 1 token: {lotus1_token}')
    print(f'Lotus 2: {lotus2_rpc}')
    print(f'Lotus 2 token: {lotus2_token}')

    # @todo, need to investgate transfer across lotus nodes

    spammer1 = fil_spammer_rpc(lotus1_rpc, lotus1_token, BASE_PATH)

    print('Genesis wallet')
    print(spammer1.get_genesis_wallet())

    if args.step == '1_create_wallets':
        print('Executing step 1, creating wallets on lotus nodes')
        spammer1.create_wallets()
        spammer2.create_wallets()

    # @todo, need to call Filecoin.StateGetActor to make sure they exist before we start transferring the funds


    # Sleep for a while to ensure the funds are properly transferred
    time.sleep(15)

    # Get and print the ChainHead CID
    chainhead_cid = spammer.get_chainhead()
    print(f'ChainHead CID: {chainhead_cid}')

    # Send multiple transactions
    nonce = 0
    for i in range(num_transactions):
        from_wallet = wallets[i % len(wallets)]
        to_wallet = wallets[(i + 1) % len(wallets)]
        amount = str(100 + (i % 100))  # Example varying amounts

        # Randomly decide whether to fuzz or send a good transaction
        if random.random() < 0.5:  # 50% chance to fuzz
            spammer.fuzz_push_message(from_wallet, to_wallet, amount, nonce)
        else:
            res = spammer.push_message(from_wallet, to_wallet, amount, nonce)
            print(f'Push message {i+1} from {from_wallet} to {to_wallet} response:', res['response_json'])

    # res = spammer.transfer_from_genesis('t1y22of2obqrgrqnswiicjpwlwhtyb7szjkefhfii', '100000')
    # print(res)
    # print(res['response'].text)

    print('done')
