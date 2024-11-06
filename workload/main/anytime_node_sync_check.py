#!/usr/bin/env -S python3 -u

import time, sys
sys.path.append("/opt/antithesis/resources")
sys.path.append("/opt/antithesis/sdk")
import nodes
from rpc import sync_state
from antithesis_sdk import antithesis_fallback_sdk


sdk = antithesis_fallback_sdk()
sdk.reachable(declare=True, id="Script execution: 'anytime_node_sync_check' ran", message="Script execution: 'anytime_node_sync_check' ran")

#todo: update to "human readable" for the assertion below
sdk.always(declare=True, id="Node is syncing check", message="Node is syncing check")

def node_sync_check():

    node = nodes.select_random_node()
    rpc_url, auth_token = nodes.get_url_and_token(node)

    old_active_syncs = sync_state(node, rpc_url, auth_token)
    sync_workers = [i["WorkerID"] for i in old_active_syncs if i["Base"] != i["Target"]]

    if not sync_workers:
        print("Workload [anytime_node_sync_check.py]: No active sync workers. Exiting")
        return

    time.sleep(10)

    new_active_syncs = sync_state(node, rpc_url, auth_token)

    for sync_worker in sync_workers:

        new_active_sync = next((i for i in new_active_syncs if i["WorkerID"] == sync_worker), {})

        if not new_active_sync:
            print(f"Workload [anytime_node_sync_check.py]: There were no new active sync workers with a workerID that matched {asw}. Exiting")
            return

        old_active_sync = next((i for i in old_active_syncs if i["WorkerID"] == sync_worker))
        
        old_base = old_active_sync["Base"]
        old_target = old_active_sync["Target"]
        new_base = new_active_sync["Base"]
        new_target = new_active_sync["Target"]

        # Are there other cases here? What is target is different but the base is the same? What is base is the same but target is different? Ask Parth about this.

        if old_base == new_base and old_target == new_target:
            print(f"Workload [anytime_node_sync_check.py]: error! Worker {sync_worker} is stuck with the same Base and Target mismatch (Base: {old_base}, Target: {old_target})")
            sdk.always(declare=False, id="Node is syncing check", message="Node is syncing check", condition=False, details={"message":"A node is stuck and is out of sync. Base and Target mismatch is the same after 10 seconds","Old active sync":old_active_sync,"New active sync":new_active_sync})
        else:
            # not stuck here?
            print(f"Workload [anytime_node_sync_check.py]: Worker {sync_worker} Base and Target changed after 10 seconds (Old Base: {old_base}, New Base: {new_base}, Old Target: {old_target}, New Target: {new_target})")
            sdk.always(declare=False, id="Node is syncing check", message="Node is syncing check", condition=True)

    sdk.reachable(declare=False, id="Script execution: 'anytime_node_sync_check' ran", message="Script execution: 'anytime_node_sync_check' ran", condition=True)


node_sync_check()


''' example activesync:
{
    'ActiveSyncs': [
        {
            'WorkerID': 13, 
            'Base': None, 
            'Target': None, 
            'Stage': 0, 
            'Height': 0, 
            'Start': '0001-01-01T00:00:00Z', 
            'End': '0001-01-01T00:00:00Z', 
            'Message': ''
        }, 
        {
            'WorkerID': 14, 
            'Base': None, 
            'Target': None, 
            'Stage': 0, 
            'Height': 0, 
            'Start': '0001-01-01T00:00:00Z', 
            'End': '0001-01-01T00:00:00Z', 
            'Message': ''
        }, 
        {
            'WorkerID': 15, 
            'Base': None, 
            'Target': None, 
            'Stage': 0, 
            'Height': 0, 
            'Start': '0001-01-01T00:00:00Z', 
            'End': '0001-01-01T00:00:00Z', 
            'Message': ''
        }
    ], 
    'VMApplied': 49
}
'''