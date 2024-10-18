from raw_request import request
from rpc import get_genesis_wallet_rpc
import json, time

def get_genesis_wallet_with_retry_and_backoff(rpc_url:str, auth_token:str):

    for i in range(1, 21):
        genesis_wallet = get_genesis_wallet_rpc(rpc_url, auth_token)
        if not genesis_wallet:
            time.sleep(i)
            print(f"Workload [Genesis Wallet]: Attempt {i} failed... retrying...")
        else:
            return genesis_wallet

    print("Workload [Genesis Wallet]: Failed to find genesis wallet after 20 attempts with backoff. This is a major issue.")
    return None
