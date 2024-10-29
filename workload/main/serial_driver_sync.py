#!/usr/bin/env -S python3 -u

import time, sys
sys.path.append("/opt/antithesis/resources")
sys.path.append("/opt/antithesis/sdk")
import nodes
from rpc import print_Sync_Status
from antithesis_sdk import antithesis_fallback_sdk


sdk = antithesis_fallback_sdk()
sdk.always(declare=True, id="Node is in sync", message="Node is in sync increases as expected")
sdk.reachable(declare=True, id="Successful 'sync.py' script execution", message="Successful 'create_wallets.py' script execution")

def check_sync():

    rpc_url, auth_token = nodes.get_url_and_token("lotus")
    initial_sync = print_Sync_Status("lotus", rpc_url, auth_token)

    initial_base_target = [(sync.get("WorkerID"), sync.get("Base"), sync.get("Target")) for sync in initial_sync]

    for worker_id, base, target in initial_base_target:
        if base != target:
            print(f"Worker {worker_id} has Base and Target mismatched initially (Base: {base}, Target: {target}).")
            time.sleep(10) 
            new_syncs = print_Sync_Status()
            new_base_target = [(sync.get("WorkerID"), sync.get("Base"), sync.get("Target")) for sync in new_syncs]
            for new_worker_id, new_base, new_target in new_base_target:
                if new_worker_id == worker_id:
                    if new_base == base and new_target == target:
                        print(f"Error: Worker {worker_id} is stuck with the same Base and Target mismatch (Base: {base}, Target: {target}).")
                        sdk.always(declare=False, id="Sync Status", message="Node is out of sync", condition=False)
                    else:
                        print(f"Worker {worker_id} Base and Target changed after wait (New Base: {new_base}, New Target: {new_target}).")
                        sdk.always(declare=False, id="Node in sync", message="Node is in sync", condition=True)
    
      

    sdk.reachable(declare=False, id="Successful 'sync.py' script execution", message="Successful 'sync' script execution", condition=True)



check_sync()