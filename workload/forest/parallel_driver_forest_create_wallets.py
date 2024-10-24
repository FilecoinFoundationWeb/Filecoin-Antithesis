#!/usr/bin/env -S python3 -u

import random, sys
sys.path.append("/opt/antithesis/resources")

import wallets
from rpc_url_and_auth_token import get_url_and_token


def create_wallets(n:int):
    
    node_type = "forest"

    rpc_url, auth_token = get_url_and_token(node_type=node_type)
    new_wallets = wallets.create_wallets(node_type=node_type, rpc_url=rpc_url, auth_token=auth_token, n=n)
    new_wallets_pks = wallets.get_wallets_private_keys(node_type=node_type, rpc_url=rpc_url, auth_token=auth_token, wallets=new_wallets)
    wallet_pk_dict = dict(zip(new_wallets, new_wallets_pks))
    wallets.write_wallets_locally(wallet_pk=wallet_pk_dict)
    print(f"Workload [Forest][create_wallets.py]: a completed wallet creation script")


n_wallets = random.SystemRandom().randint(5, 15)

create_wallets(n=n_wallets)
