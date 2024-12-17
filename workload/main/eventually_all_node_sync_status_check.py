#!/usr/bin/env -S python3 -u

import time, sys
sys.path.append("/opt/antithesis/resources")
sys.path.append("/opt/antithesis/sdk")
import nodes
from rpc import get_chainhead
from antithesis_sdk import antithesis_fallback_sdk


sdk = antithesis_fallback_sdk()

sdk.always(declare=True, id="All nodes are synced (within 1) during period of no faults", message="All nodes are synced (within 1) during period of no faults")

def all_node_sync_status():

    # waiting for a period of time for the system to recover. 
    time.sleep(30)

    all_nodes = nodes.get_all_nodes()
    node_height_dict = {f"{node}_height": None for node in all_nodes}

    for node in all_nodes:
        rpc_url, auth_token = nodes.get_url_and_token(node)
        
        chainhead = get_chainhead(node, rpc_url, auth_token)

        if not chainhead:
            return

        node_height_dict[f"{node}_height"] = chainhead['result']['Height']

    # each height is an int
    within_one = max(node_height_dict.values()) - min(node_height_dict.values()) <= 1

    if within_one:
        sdk.always(declare=False, id="All nodes are synced (within 1) during period of no faults", message="All nodes are synced (within 1) during period of no faults", condition=True, details=node_height_dict)
    else:
        sdk.always(declare=False, id="All nodes are synced (within 1) during period of no faults", message="All nodes are synced (within 1) during period of no faults", condition=False, details=node_height_dict)


all_node_sync_status()
