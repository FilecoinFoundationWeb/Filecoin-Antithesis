#!/usr/bin/env -S python3 -u

from nodes import get_url_and_token
import wallets, transaction
import sys
sys.path.append("/opt/antithesis/sdk")
from antithesis_sdk import antithesis_fallback_sdk

sdk = antithesis_fallback_sdk()
sdk.reachable(declare=True, id="Successful 'initialize_wallets.py' script execution", message="Successful 'initialize_wallets.py script execution")

def init_wallets(node_type):

    # get genesis wallet & pk, use lotus node? not sure if I can do any of this on the forest node
    lotus_rpc_url, lotus_auth_token = get_url_and_token(node_type="lotus-1")
    genesis_wallet = wallets.get_genesis_wallet(node_type="lotus-1", rpc_url=lotus_rpc_url, auth_token=lotus_auth_token)
    genesis_wallet_pk = wallets.get_wallets_private_keys(node_type=node_type, rpc_url=lotus_rpc_url, auth_token=lotus_auth_token, wallets=[genesis_wallet])[0]

    # get rpc_url and auth_token for node
    rpc_url, auth_token = get_url_and_token(node_type=node_type)

    # create new wallets and get their pks
    new_wallets = wallets.create_wallets(node_type=node_type, rpc_url=rpc_url, auth_token=auth_token, n=10)
    new_wallets_pks = wallets.get_wallets_private_keys(node_type=node_type, rpc_url=rpc_url, auth_token=auth_token, wallets=new_wallets)

    # zipping the wallets and private keys into a dictionary for storage
    wallet_pk_dict = dict(zip(new_wallets, new_wallets_pks))

    # writing wallets locally
    wallets.write_wallets_locally(wallet_pk=wallet_pk_dict)

    # giving initial wallets some FIL
    transaction.feed_wallets(node_type, rpc_url, auth_token, genesis_wallet, genesis_wallet_pk, new_wallets, 40000)

    # reached the end of the script without error
    sdk.reachable(declare=False, id="Successful 'initialize_wallets.py' script execution", message="Successful 'initialize_wallets.py script execution", condition=True, details={"node":node_type})
  

if __name__ == "__main__":
    if len(sys.argv) != 2:
        print("Error: Invalid parameters were passed to initialize_wallets.py")
    else:    
        init_wallets(sys.argv[1])

