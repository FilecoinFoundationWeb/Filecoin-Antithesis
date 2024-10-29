#!/usr/bin/env -S python3 -u

import random, time, sys
sys.path.append("/opt/antithesis/resources")
sys.path.append("/opt/antithesis/sdk")
from wallets import get_random_wallets
import nodes
from transaction import make_transaction
from antithesis_sdk import antithesis_fallback_sdk


sdk = antithesis_fallback_sdk()
sdk.reachable(declare=True, id="Successful 'spammer.py' script execution", message="Successful 'spammer.py' script execution")

def spam_hard(n:int, n_wallets:int=10):
    node = nodes.select_random_node()
    print(f"Workload [main][spammer.py]: entering a spamming script for {n} transactions for {node}")
    rpc_url, auth_token = nodes.get_url_and_token(node_type=node)
    wallets = get_random_wallets(n_wallets)
    if not wallets:
        print(f"Workload [main][spammer.py]: Not enough wallets available. exiting.")
        return
    print(f"Workload [main][spammer.py]: Selected {n_wallets} random wallets")
    for i in range(n):
        print(f"Workload [main][spammer.py]: iteration {i+1} / {n}")
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
        print(f"Workload [main][spammer.py]: amount of attoFIL for the next transaction: {amount}")
        print("Workload [main][spammer.py]: executing a transaction")
        make_transaction(node_type, rpc_url, auth_token, from_w, from_pk, to_w, amount)
        print("Workload [main][spammer.py]: finished a transaction")
    sdk.reachable(declare=False, id="Successful 'spammer.py' script execution", message="Successful 'spammer.py' script execution", condition=True, details={"node":node})


transaction_options = [10, 25, 50, 80, 120, 200]

spam_hard(n=random.SystemRandom().choice(transaction_options))
