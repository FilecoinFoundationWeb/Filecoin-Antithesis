import time, random
from filelock import FileLock
import rpc


def get_genesis_wallet(node_type:str, rpc_url:str, auth_token:str) -> str:
    '''
    @purpose - get the id hash of the genesis wallet
    @param rpc_url - endpoint address for node
    @param auth_token - authentication token for that node
    @return - id hash of genesis wallet
    '''
    backoff = 0
    print(f"Workload [wallets.py]: attempting to get genesis wallet id hash")
    while True:
        genesis_wallet = rpc.get_genesis_wallet(node_type, rpc_url, auth_token)
        if genesis_wallet:
            print(f"Workload [wallets.py]: successfully got genesis wallet id hash")
            return genesis_wallet
        print(f"Workload [wallets.py]: attempt {backoff + 1} failed to get genesis wallet, retrying")
        backoff += 1
        if backoff >= 16:
            print(f"Workload [wallets.py]: failed to get genesis wallet after a long time on a {node_type} node. this is a serious issue. returning None")
            return None
        time.sleep(backoff)


def get_wallets_private_keys(node_type:str, rpc_url:str, auth_token:str, wallets:list) -> list:
    '''
    @purpose - get private keys for a list of wallets
    @param rpc_url - endpoint address for node
    @param auth_token - authentication token for that node
    @param wallets - list of wallets to get private keys for
    @return - list of private keys from the wallets that match in sequence with original list of wallets
    '''
    num_wallets = len(wallets)
    private_keys = []
    pks_found, backoff = 0, 0
    print(f"Workload [wallets.py]: attempting to get {num_wallets} private keys")
    while pks_found < num_wallets:
        if backoff >= 16:
            print(f"Workload [wallets.py]: failed to get private key after a long time on a {node_type} node. this is a serious issue. returning None")
            return None
        pk = rpc.get_wallet_private_key(node_type, rpc_url, auth_token, wallets[pks_found])
        if pk:
            pks_found += 1
            print(f"Workload [wallets.py]: appending pk to the private_key list. Progress: {pks_found} / {num_wallets}.")
            private_keys.append(pk)
            backoff = 0
        else:
            backoff += 1
            print(f"Workload [wallets.py]: get private key failed. retrying... attempt {backoff+1} for pk #{pks_found+1}.")
            time.sleep(backoff)
    print(f"Workload [wallets.py]: successfully got private keys")
    return private_keys


def create_wallets(node_type:str, rpc_url:str, auth_token:str, n:int) -> list:
    '''
    @purpose - create a certain number of wallets
    @param rpc_url - endpoint address for node
    @param auth_token - authentication token for that node
    @param n - # of wallets to create
    @return - list of wallet hash ids that were created
    '''
    wallets = []
    wallets_created, backoff = 0, 0
    print(f"Workload [wallets.py]: attempting to create {n} wallets")
    while wallets_created < n:
        if backoff >= 16:
            print(f"Workload [wallets.py]: failed to create a wallet after a long time on a {node_type} node. this is a serious issue. returning None")
            return None
        wallet = rpc.create_wallet(node_type, rpc_url, auth_token)
        if wallet:
            wallets_created += 1
            print(f"Workload [wallets.py]: appending wallet to the wallets list. Progress: {wallets_created} / {n}")
            wallets.append(wallet)
            backoff = 0
        else:
            backoff += 1
            print(f"Workload [wallets.py]: wallet creation failed. retrying... attempt {backoff+1} for wallet #{wallets_created+1}.")
            time.sleep(backoff)
    print(f"Workload [wallets.py]: successfully created wallets")
    return wallets


def delete_wallets(node_type:str, rpc_url:str, auth_token:str, wallets_to_delete:list):
    num_wallets_to_delete = len(wallets_to_delete)
    wallets_deleted, backoff = 0, 0
    print(f"Workload [wallets.py]: attempting to delete {num_wallets_to_delete} wallets")
    while wallets_deleted < num_wallets_to_delete:
        if backoff >= 16:
            print(f"Workload [wallets.py]: failed to delete a wallet after a long time on a {node_type} node. this is a serious issue. returning")
            return
        response = rpc.delete_wallet(node_type, rpc_url, auth_token, wallets_to_delete[wallets_deleted])
        if response:
            wallets_deleted += 1
            print(f"Workload [wallets.py]: deleted wallet. Progress: {wallets_deleted} / {num_wallets_to_delete}")
            backoff = 0
        else:
            backoff += 1
            print(f"Workload [wallets.py]: failed to delete wallet. retrying... attempt {backoff+1} for wallet #{wallets_deleted+1}.")
            time.sleep(backoff)
    print(f"Workload [wallets.py]: successfully deleted wallets")


def write_wallets_locally(wallet_pk:dict):
    '''
    @purpose - write a dict of wallets and private keys to wallets.txt this will help persist the wallets across states
    @param wallet_pk - dict of {wallet:private_key}
    '''
    print("Workload [wallets.py]: attempting to store wallets locally")
    with FileLock("/opt/antithesis/resources/wallets.txt.lock"):
        with open("/opt/antithesis/resources/wallets.txt", "a") as f:
            for w, pk in wallet_pk.items():
                f.write("%s:%s\n" % (w,pk))
    print("Workload [wallets.py]: successfully stored wallets locally")


def delete_wallets_locally(wallet_pk:dict):
    '''
    @purpose - delete a dict of wallets and private keys from wallets.txt
    @param wallet_pk - dict of {wallet:private_key}
    '''
    print("Worklod [wallets.py]: attempting to delete wallets locally")
    wallet_pk_list = [f"{w}:{pk}" for w, pk in wallet_pk.items()]
    with FileLock("/opt/antithesis/resources/wallets.txt.lock"):
        with open("/opt/antithesis/resources/wallets.txt", "r") as f:
            filtered_lines = [line for line in f if line.strip() not in wallet_pk_list]
        with open("/opt/antithesis/resources/wallets.txt", "w") as f:
            f.writelines(filtered_lines)
    print("Workload [wallets.py]: successfully deleted wallets locally")
        

def get_random_wallets(num:int) -> dict:
    '''
    @purpose - get a dictionary of random wallet:private_key pairs of size num, can't be bigger than existing local wallets
    @param num - number of random wallets to take, without replacement
    @return - dict from 2 lines above ^^
    '''
    print(f"Workload [wallets.py]: attempting to get {num} random wallets")
    with FileLock("/opt/antithesis/resources/wallets.txt.lock"):
        with open("/opt/antithesis/resources/wallets.txt", "r") as f:
            line_count = sum(1 for line in f)
    if num > line_count:
        print(f"Workload [wallets.py]: Your {num} is > {line_count} wallets available in wallets.txt. returning None")
        return None
    with FileLock("/opt/antithesis/resources/wallets.txt.lock"):
        with open("/opt/antithesis/resources/wallets.txt", "r") as f:
            lines = f.readlines()
    random_lines = random.SystemRandom().sample(lines, num)
    random_wallets = [line.strip() for line in random_lines]
    random_wallets_dict = {}
    for r_w in random_wallets:
        w, pk = r_w.split(":")
        random_wallets_dict[w] = pk
    print(f"Workload [wallets.py]: successfully got {num} random wallets")
    return random_wallets_dict
