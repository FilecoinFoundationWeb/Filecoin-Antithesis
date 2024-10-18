#!/usr/bin/env -S python3 -u

import random, time, sys
sys.path.append("/opt/antithesis/resources")

from wallets import get_random_wallets
from lotus_rpc_token import get_lotus_url_token
from transaction import make_transaction


transaction_options = [10, 25, 50, 80, 120, 200]


def spam_hard(n:int, n_wallets:int):

    print(f"Workload [spammer.py]: entering a spamming script for {n} transactions")
    lotus_rpc_url, lotus_auth_token = get_lotus_url_token()
    wallets = get_random_wallets(n_wallets)
    if not wallets:
        print(f"Workload [spammer.py]: Not enough wallets available. exiting.")
        return
    print(f"Workload [spammer.py]: Selected {n_wallets} random wallets")
    for i in range(n):
        print(f"Workload [spammer.py]: iteration {i+1} / {n}")
        nominal_amount = 100
        last_seed = time.time()
        if (last_seed + 5 < time.time()):
            with open('/dev/urandom', 'rb') as f:
                random_bytes = f.read(1)
                seed_value = int.from_bytes(random_bytes, byteorder='big')
                random.SystemRandom().seed(seed_value)
        from_w, from_pk = random.SystemRandom().choice(list(wallets.items()))
        to_w, _ = random.SystemRandom().choice(list(wallets.items()))
        amount = int(float(random.SystemRandom().gauss(nominal_amount, nominal_amount ** (1/2))))
        print(f"Workload [spammer.py]: amount of attoFIL for the next transaction: {amount}")
        print("Workload [spammer.py]: executing a transaction")
        make_transaction(lotus_rpc_url, lotus_auth_token, from_w, from_pk, to_w, amount)
        print("Workload [spammer.py]: finished a transaction")
    print("Workload [spammer.py]: a completed transaction spamming script")

spam_hard(n=random.SystemRandom().choice(transaction_options), n_wallets=10)
