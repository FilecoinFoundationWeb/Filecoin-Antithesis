#!/usr/bin/env -S python3 -u

import random, sys
sys.path.append("/opt/antithesis/resources")

import wallets
from lotus_rpc_token import get_lotus_url_token


def delete_wallets(n:int):
    lotus_rpc_url, lotus_auth_token = get_lotus_url_token()
    wallets_to_delete = wallets.get_random_wallets(num=n)
    if not wallets_to_delete:
        return
    wallets.delete_wallets_locally(wallets_to_delete)
    wallets_to_delete = list(wallets_to_delete.keys())
    wallets.delete_wallets(lotus_rpc_url, lotus_auth_token, wallets_to_delete)
    print(f"Workload [delete_wallets.py]: a completed wallet deletion script")


n_wallets = random.SystemRandom().randint(3, 6)

delete_wallets(n=n_wallets)
