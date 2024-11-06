#!/usr/bin/env -S python3 -u

import random, sys
sys.path.append("/opt/antithesis/resources")
sys.path.append("/opt/antithesis/sdk")
import wallets, nodes
from antithesis_sdk import antithesis_fallback_sdk


sdk = antithesis_fallback_sdk()
sdk.reachable(declare=True, id="Script execution: 'delete_wallets' ran", message="Script execution: 'delete_wallets' ran")

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
    sdk.reachable(declare=False, id="Script execution: 'delete_wallets' ran", message="Script execution: 'delete_wallets' ran", condition=True, details={"node type":node})

delete_wallets()
