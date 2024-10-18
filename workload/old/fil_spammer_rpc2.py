import requests, json, random, argparse, os, time, math, threading
time.sleep(120)
    
class fil_spammer_rpc():
    '''
    spamming fil across a local network
    '''
    def __init__(self, rpc_url:str, auth_token:str, common_mount_path:str, label: str):
        '''
        The auth token should be generated from  the cli before starting the workload
        lotus auth create-token --perm <read,write,sign,admin>
        https://lotus.filecoin.io/reference/basics/api-access/
        '''
        self.auth_token = auth_token
        # url:1234/rpc/v0
        self.rpc_url = rpc_url
        self.basepath = common_mount_path
        self.label = label
        

    def do_request(self, method:str, payload:dict) -> dict:
        '''
        Making raw api requests
        @param method - get | post
        @param payload - request payload
        '''
        # default headers
        headers = {
            "Content-Type": "application/json",
            # "Accept": "application/json",
            "Authorization": f'Bearer {self.auth_token}'
        }

        if method in ['get', 'post', 'put', 'delete', 'head', 'options']:
            # @todo: need to provide stuffing of additional kwargs
            # payloads are mapped differently in the request call
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

            return {
                'request': {
                    'url': self.rpc_url,
                    'headers': headers,
                    'payload': payload,
                },
                'response': response
            }

    def read_wallets(self, node:str = 'lotus-1') -> str:
        wallets = []
        with open(f'{self.basepath}/wallet-{node}.json', 'r') as fh:
            wallets = json.load(fh)

        return wallets

    def _estimate_message_gas(self, wallet_from_id:str, wallet_from_pk: str, wallet_to_id:str, amount:str) -> dict:
        '''
        Estiamte gas for a mPool message
        return a dictionary with
        Nonce, Value, GasLimit, GasFeeCap, GasPremium
        '''
        method = 'Filecoin.GasEstimateMessageGas'
        payload = json.dumps({
            "jsonrpc": "2.0",
            "id": "1",
            "method": method,
            "params": [
                {
                    # "Version": 42,
                    "To": wallet_to_id,
                    "From": wallet_from_id,
                    "Value": amount,
                    "Method": 0,
                    "Params": wallet_from_pk,
                    "GasLimit": 10000000
                },
                {
                    "MaxFee": "0"
                }, None
            ]
        })

        res = self.do_request('post', payload)
        print(res)
        print(res['response'].json())
        if res['response'].status_code != 200:
            print(f'Bad response from {method}')
            return False

        data = res['response'].json()
        return data['result']


    def transfer(self, wallet_from_id:str, wallet_from_pk:str, wallet_to_id: str, amount:int, bugify: bool):

        amount = str(amount*10**18)
        print(f"{self.label}: starting transfer")
        cid = self.get_chainhead()
        if (not bool(cid)):
            print(f'{self.label}: Failed to get CID from chainhead RPC call')
            return False

        gas_info = self._estimate_message_gas(wallet_from_id, wallet_from_pk, wallet_to_id, amount)
        if (not bool(gas_info)):
            print(f'{self.label} Failed to get gas information to do MpoolPushMessage')
            return False

        method = 'Filecoin.MpoolPushMessage'
        
        print(f"{self.label}: GasLimit: {gas_info['GasLimit']}")
        print(f"{self.label}: GasFeeCap: {gas_info['GasFeeCap']}")
        print(f"{self.label}: GasPremium: {gas_info['GasPremium']}")
        gas_limit_offset = 0
        gas_fee_offset = 0
        gas_premium_offset = 0
        bad = random.randint(0,10) < 3
        if bugify and bad:
            print(f"{self.label}: BUGIFY: The next transaction might not go through")
            gl = int(float(gas_info['GasLimit']))
            gfc = int(float(gas_info['GasFeeCap']))
            gp = int(float(gas_info['GasPremium']))
            gas_limit_offset = random.randint(-gl, 0)
            gas_fee_offset = random.randint(-gfc, 0)
            gas_premium_offset = random.randint(-gp, 0)
            print(f"{self.label}: gas_limit_offset: {gas_limit_offset}")
            print(f"{self.label}: gas_fee_offset: {gas_fee_offset}")
            print(f"{self.label}: gas_premium_offset: {gas_premium_offset}")
        
        payload = json.dumps({
            "jsonrpc": "2.0",
            "id": "1",
            "method": method,
            "params": [
                {
                    # "Version": 42,
                    "To": wallet_to_id,
                    "From": wallet_from_id,
                    "Value": amount,
                    # "GasLimit": gas_info['GasLimit'],
                    # "GasFeeCap": gas_info['GasFeeCap'],
                    # "GasPremium": gas_info['GasPremium'],
                    "GasLimit": int(float(gas_info['GasLimit']) + gas_limit_offset),
                    "GasFeeCap": str(int(float(gas_info['GasFeeCap']) + gas_fee_offset)),
                    "GasPremium": str(int(float(gas_info['GasPremium']) + gas_premium_offset)),
                    "Method": 0,
                    "Params": wallet_from_pk,
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
        res = self.do_request('post', payload)
        if bugify and bad:
            print(f"{self.label}: BUGIFY: transfer response: {res}")
        else:
            print(f"{self.label}: transfer response: {res}")
        if res['response'].status_code != 200:
            if bugify and bad:
                print(f'{self.label}: BUGIFY: Bad response from {method}')
            else:
                print(f'{self.label}: Unexpected: Bad response from {method}')
            return False
        if bugify and bad:
            print(f"{self.label}: BUGIFY: transfer success response object: {res['response'].json()}")
        else:
            print(f"{self.label}: transfer success response object: {res['response'].json()}")

        return res

    def create_wallet(self) -> str:
        '''
        @param lotus_node - the lotus node in which the wallet is created
        @return the wallet id created
        '''
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
            return False

        data = res['response'].json()

        return data['result']
    
    def create_wallets(self, n) -> [str]:
        wallets = []
        for _ in range(0, n):
            backoff = 0
            while True: 
                w = self.create_wallet()
                print(w)
                if w != False:
                    break
                backoff += 1
                time.sleep(backoff)
            wallets.append(w)
        return wallets
    
    def create_wallet_pks(self, wallets) -> [str]:
        wallet_pks = []
        for w in wallets:
            backoff = 0
            while True: 
                pk = self.get_wallet_pk(w)
                print(f"pk: {pk}")
                if pk != False:
                    break
                backoff += 1
                time.sleep(backoff)
            wallet_pks.append(pk)
        return wallet_pks

    def get_wallet_pk(self, wallet_id) -> str:
        '''
        Getting the private key for a wallet so we can use it to send transactions
        '''
        print("hi")
        method = 'Filecoin.WalletExport'
        payload = json.dumps({
            "jsonrpc": "2.0",
            "id": "1",
            "method": method,
            "params": [wallet_id]
        })
        res = self.do_request('post', payload)
        
        print(res['response'].text)

        if res['response'].status_code != 200:
            print(f'Bad response from {method}')
            return False

        data = res['response'].json()
        print(data)

        return data['result']['PrivateKey']


    def get_chainhead(self):
        '''
        This method doesn't need auth
        @return cid that we can potentially use to push messages
        '''
        cid = ''
        method = 'Filecoin.ChainHead'
        payload = json.dumps({
            "jsonrpc": "2.0",
            "id": "1",
            "method": method
        })
        res = self.do_request('post', payload)

        if res['response'].status_code != 200:
            print(f'Bad response from {method}')
            return False

        response_body = res['response'].json()
        
        print(response_body)

        if len(response_body['result']['Cids']) > 0:
            cid = response_body['result']['Cids'][0]
            # pretty bad here
            if '/' in cid:
                cid = cid['/']

        return cid
        
    def get_genesis_wallet(self):
        '''
        Get the wallet address of the genesis wallet which has all of the FIL we need to transfer
        '''
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
            return False

        # Crude logic to retrieve the genesis wallet, should be on the primary leader node
        example_non_genesis_wallet = "t1sua7sz4h43j3rhsh3x7f5ciysyck7bqzme3dtxy"

        response_body = res['response'].json()
        for wallet in response_body['result']:
            if len(wallet) > len(example_non_genesis_wallet):
                genesis_wallet = wallet

        return genesis_wallet
    
    
    def wealth_transfer(self, genesis_wallet: str, genesis_wallet_pk: str, wallets: [str], amount: int):
        for w in wallets:
            backoff = 0
            while True: 
                complete = self.transfer(genesis_wallet, genesis_wallet_pk, w, amount, False)
                if complete != False:
                    break
                backoff += 1
                time.sleep(backoff)
    
    def spam_hard(self, wallets: [str], wallet_pks: [str], nominal_amount: int):
        while (True):
            last_seed = time.time();
            if (last_seed + 5 < time.time()): 
                with open('/dev/urandom', 'rb') as f:
                    # Read 1 bytes from /dev/urandom (adjust the number of bytes as needed)
                    random_bytes = f.read(1)
                    # Convert the random bytes to an integer
                    seed_value = int.from_bytes(random_bytes, byteorder='big')
                    random.seed(seed_value)

            i_from = random.randint(1, len(wallets)) - 1
            wallet_from = wallets[i_from]
            wallet_from_pk = wallet_pks[i_from]
            print(f"wallet_from: {wallet_from}")
            
            i_to = random.randint(1, len(wallets)) - 1
            wallet_to = wallets[i_to]
            print(f"wallet_to: {wallet_to}")

            
            amount = int(float(random.gauss(nominal_amount, nominal_amount ** (1/2))))
            print(amount)
            self.transfer(wallet_from, wallet_from_pk, wallet_to, amount, True)

        
    
def get_lotus_rpc_auth_token(token_path:str) -> str:
    '''
    Quick helper to get the auth token
    '''
    auth_token = 'lll'
    with open(token_path) as f:
        auth_token = f.read()

    auth_token = auth_token.rstrip()

    return auth_token
    
def read_all_wallets():
    wallets_group = []
    file_count = int(os.getenv("LOTUS_COUNT"))
    print(file_count)
    while True:
        files = os.listdir("/opt/lotus_transformed/customer/devgen/")
        matching_files = [file for file in files if file.startswith("wallets_in_")]
        ready_files = [file for file in files if file.startswith("wallets_ready_in_")]
        
        print(ready_files)
        if len(ready_files) == file_count:
            print(f"All wallets have been generated")
            break

        time.sleep(5)    
        print("Waiting for wallets to be generated")

    for w in matching_files:
        wallets = []
        with open(f"/opt/lotus_transformed/customer/devgen/{w}", 'r') as file:
            for line in file:
                wallets.append(line.strip())
        print(f"{w}: {wallets}")
        wallets_group.append(wallets)
    return wallets_group

    
# print(wg)
lotus1_rpc = "http://10.20.20.24:1234/rpc/v0"
TOKEN_LOTUS_1 = "jwt"
#TOKEN_LOTUS_2 = "lotus-2-token.txt"
BASE_PATH = '/root/devgen'
# wg = read_all_wallets(BASE_PATH)
lotus1_token = get_lotus_rpc_auth_token(f'{BASE_PATH}/{TOKEN_LOTUS_1}')
#lotus2_token = get_lotus_rpc_auth_token(f'{BASE_PATH}/{TOKEN_LOTUS_2}')
print(lotus1_token)
spammer1 = fil_spammer_rpc(lotus1_rpc, lotus1_token, BASE_PATH, 'lotus-1')
#spammer2 = fil_spammer_rpc(lotus2_rpc, lotus2_token, BASE_PATH, 'lotus-2')
    
genesis_wallet = spammer1.get_genesis_wallet()
genesis_wallet_pk = spammer1.get_wallet_pk(genesis_wallet)
print(f"genesis wallet pk : {genesis_wallet_pk}")
wallets1 = spammer1.create_wallets(5)
#print(f"lotus1 wallets: {wallets1}")
#wallets2 = spammer2.create_wallets(5)
#print(f"lotus2 wallets: {wallets2}")
pks1 = spammer1.create_wallet_pks(wallets1)
#pks2 = spammer2.create_wallet_pks(wallets2)
wallets = wallets1
pks = pks1
spammer1.wealth_transfer(genesis_wallet,genesis_wallet_pk, wallets, 4000000)
# spammer2.wealth_transfer(genesis_wallet,genesis_wallet_pk, wallets2, 4000000)
c = 15
while c:
    print(c)
    c -= 1
    time.sleep(1)
print("Wealth transfer complete")
try:
    print("hi")
    s1 = threading.Thread(target=spammer1.spam_hard, name='spammer1', args=[wallets, pks, 100])
    s1.start()
    spammer2.spam_hard(wallets, pks, 100)
except Exception as e:
    print(e.trace)
