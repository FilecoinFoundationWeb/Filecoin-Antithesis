#!/usr/bin/env -S python3 -u

import random, sys
sys.path.append("/opt/antithesis/resources")
import wallets, nodes

from antithesis.assertions import (
    reachable,
)

def delete_wallets():
    n_wallets = random.SystemRandom().randint(3, 6)
    node = nodes.select_random_node()
    rpc_url, auth_token = nodes.get_url_and_token(node_type=node)
    wallets_to_delete = wallets.get_random_wallets(num=n_wallets)
    if not wallets_to_delete:
        print(f"Workload [main][delete_wallets.py]: Not enough wallets available. exiting.")
        return
    wallets.delete_wallets_locally(wallets_to_delete)
    wallets_to_delete = list(wallets_to_delete.keys())
    wallets.delete_wallets(node, rpc_url, auth_token, wallets_to_delete)

delete_wallets()
