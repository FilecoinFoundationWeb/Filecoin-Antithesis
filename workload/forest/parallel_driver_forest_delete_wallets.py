#!/usr/bin/env -S python3 -u

import random, sys
sys.path.append("/opt/antithesis/resources")

import wallets
from rpc_url_and_auth_token import get_url_and_token


def delete_wallets(n:int):
    
    node_type="forest"

    rpc_url, auth_token = get_url_and_token(node_type=node_type)
    wallets_to_delete = wallets.get_random_wallets(num=n)
    if not wallets_to_delete:
        return
    wallets.delete_wallets_locally(wallets_to_delete)
    wallets_to_delete = list(wallets_to_delete.keys())
    wallets.delete_wallets(node_type, rpc_url, auth_token, wallets_to_delete)
    print(f"Workload [Forest][delete_wallets.py]: a completed wallet deletion script")


n_wallets = random.SystemRandom().randint(3, 6)

delete_wallets(n=n_wallets)
