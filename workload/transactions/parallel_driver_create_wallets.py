#!/usr/bin/env -S python3 -u

import random, sys
sys.path.append("/opt/antithesis/resources")

import wallets
from lotus_rpc_token import get_lotus_url_token


def create_wallets(n:int):
    lotus_rpc_url, lotus_auth_token = get_lotus_url_token()
    new_wallets = wallets.create_wallets(rpc_url=lotus_rpc_url, auth_token=lotus_auth_token, n=n)
    new_wallets_pks = wallets.get_wallets_private_keys(rpc_url=lotus_rpc_url, auth_token=lotus_auth_token, wallets=new_wallets)
    wallet_pk_dict = dict(zip(new_wallets, new_wallets_pks))
    wallets.write_wallets_locally(wallet_pk=wallet_pk_dict)
    print(f"Workload [create_wallets.py]: a completed wallet creation script")


n_wallets = random.SystemRandom().randint(5, 15)

create_wallets(n=n_wallets)
