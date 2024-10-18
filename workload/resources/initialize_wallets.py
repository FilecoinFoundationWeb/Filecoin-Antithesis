from lotus_rpc_token import get_lotus_url_token
import wallets, transaction

def init_wallets():

    # get rpc_url and auth_token for lotus node
    rpc_url, auth_token = get_lotus_url_token()

    # get genesis wallet & pk
    genesis_wallet = wallets.get_genesis_wallet(rpc_url=rpc_url, auth_token=auth_token)
    genesis_wallet_pk = wallets.get_wallets_private_keys(rpc_url=rpc_url, auth_token=auth_token, wallets=[genesis_wallet])[0]

    # create new wallets and get their pks
    new_wallets = wallets.create_wallets(rpc_url=rpc_url, auth_token=auth_token, n=10)
    new_wallets_pks = wallets.get_wallets_private_keys(rpc_url=rpc_url, auth_token=auth_token, wallets=new_wallets)

    # zipping the wallets and private keys into a dictionary for storage
    wallet_pk_dict = dict(zip(new_wallets, new_wallets_pks))

    # writing wallets locally
    wallets.write_wallets_locally(wallet_pk=wallet_pk_dict)

    # giving initial wallets some FIL
    transaction.feed_wallets(rpc_url, auth_token, genesis_wallet, genesis_wallet_pk, new_wallets, 40000)


if __name__ == "__main__":
    init_wallets()

