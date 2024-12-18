#!/usr/bin/env -S python3 -u

import random, time, sys
sys.path.append("/opt/antithesis/resources")
sys.path.append("/opt/antithesis/sdk")
from wallets import get_random_wallets
import nodes
from transaction import make_transaction
from antithesis_sdk import antithesis_fallback_sdk


sdk = antithesis_fallback_sdk()
sdk.reachable(declare=True, id="Script execution: 'parallel_driver_spammer' ran", message="Script execution: 'parallel_driver_spammer' ran")

def spam_hard(n_wallets:int=10):

    transaction_options = [10, 25, 50, 80, 120, 200, 500]
    n = random.SystemRandom().choice(transaction_options)
    cooldown = random.SystemRandom().choice([0, 0.25, 0.05, 0.75, 0.1])

    node = nodes.select_random_node()
    print(f"Workload [main][spammer.py]: entering a spamming script for {n} transactions with transaction cooldown {cooldown} for {node}")

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
        make_transaction(node, rpc_url, auth_token, from_w, from_pk, to_w, amount)
        time.sleep(cooldown)
    
    sdk.reachable(declare=False, id="Script execution: 'parallel_driver_spammer' ran", message="Script execution: 'parallel_driver_spammer' ran", condition=True, details={"node type":node})


spam_hard()
