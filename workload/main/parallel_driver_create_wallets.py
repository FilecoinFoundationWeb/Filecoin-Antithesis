#!/usr/bin/env -S python3 -u

import random, sys
sys.path.append("/opt/antithesis/resources")
sys.path.append("/opt/antithesis/sdk")
import wallets, nodes
from antithesis_sdk import antithesis_fallback_sdk


sdk = antithesis_fallback_sdk()
sdk.reachable(declare=True, id="Successful 'create_wallets.py' script execution", message="Successful 'create_wallets.py' script execution")

def create_wallets():
    n_wallets = random.SystemRandom().randint(5, 15)
    node = nodes.select_random_node()
    rpc_url, auth_token = nodes.get_url_and_token(node_type=node)
    new_wallets = wallets.create_wallets(node_type=node, rpc_url=rpc_url, auth_token=auth_token, n=n_wallets)
    new_wallets_pks = wallets.get_wallets_private_keys(node_type=node, rpc_url=rpc_url, auth_token=auth_token, wallets=new_wallets)
    wallet_pk_dict = dict(zip(new_wallets, new_wallets_pks))
    wallets.write_wallets_locally(wallet_pk=wallet_pk_dict)
    sdk.reachable(declare=False, id="Successful 'create_wallets.py' script execution", message="Successful 'create_wallets.py' script execution", condition=True, details={"node":node})

create_wallets()
