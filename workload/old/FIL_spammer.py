import subprocess
import os
import time
import random

example_non_genesis_wallet = "t1sua7sz4h43j3rhsh3x7f5ciysyck7bqzme3dtxy"

def create_wallets(num_wals=10, transfer=10000):
    name = os.getenv("LOTUS")
    with open(f'wallets_in_{name}.txt', 'w') as file:
        for wal in range(0, num_wals):
            res = subprocess.run(['/go/lotus-local-net/./lotus', 'wallet', 'new'], capture_output=True, text=True)
            file.write(res.stdout)


def read_genesis_wallet_from_cluster():
    res = subprocess.run(['/go/lotus-local-net/./lotus', 'wallet', 'list', '--addr-only'], capture_output=True, text=True)
    wallet_list = res.stdout.split('\n')
    genesis_wallet_id = ""
    for wallet in wallet_list:
        if len(wallet) > len(example_non_genesis_wallet):
            genesis_wallet_id = wallet
    return genesis_wallet_id


def read_all_wallets():
    wallets_group = []
    file_count = int(os.getenv("LOTUS_COUNT"))
    print(file_count)
    while True:
        files = os.listdir("./")
        matching_files = [file for file in files if file.startswith("wallets_in_")]

        print(matching_files)
        if len(matching_files) == file_count:
            print(f"All wallets have been generated")
            break

        time.sleep(5)    
        print("Waiting for wallets to be generated")

    for w in matching_files:
        wallets = []
        with open(w, 'r') as file:
            for line in file:
                wallets.append(line.strip())
        wallets_group.append(wallets)
    return wallets_group


def distribute_fil_from_genesis_wallet(genesis_wallet_id, wallets_group):
    if genesis_wallet_id != "":
        print("Distributing from genesis")
        for wallets in wallets_group:
            for wallet in wallets:
                print(["/go/lotus-local-net/./lotus", "send", wallet, "240000"])
                subprocess.run(["/go/lotus-local-net/./lotus", "send", wallet, "240000"], capture_output=True, text=True)


def spam_transactions(wallets, max_size):
    fil = random.randint(0, max_size)
    print(f"wallets: {wallets}")
    while True:
        wallet_group_from = wallets[random.randint(1, len(wallets)) - 1]
        print(f"1, {len(wallet_group_from)}")
        i = random.randint(1, len(wallet_group_from)) - 1
        wallet_from = wallet_group_from[i]

        wallet_group_to = wallets[random.randint(1, len(wallets)) - 1]
        i = random.randint(1, len(wallet_group_to)) - 1
        wallet_to = wallet_group_to[i]

        print(["/go/lotus-local-net/./lotus", "send", "--from", wallet_from, wallet_to, str(fil)])

        x = subprocess.run(["/go/lotus-local-net/./lotus", "send", "--from", wallet_from, wallet_to, str(fil)], capture_output=True, text=True)
        print(x.stdout)


genesis_wallet_id = read_genesis_wallet_from_cluster()
create_wallets()
wallets = read_all_wallets()
distribute_fil_from_genesis_wallet(genesis_wallet_id, wallets)

spam_transactions(wallets, 10)

# print(genesis_wallet_id)