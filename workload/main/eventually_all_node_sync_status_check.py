#!/usr/bin/env -S python3 -u

import time, sys
sys.path.append("/opt/antithesis/resources")
sys.path.append("/opt/antithesis/sdk")
import nodes
from rpc import get_chainhead
from antithesis_sdk import antithesis_fallback_sdk


sdk = antithesis_fallback_sdk()

sdk.reachable(declare=True, id="Script execution: 'eventually_all_node_sync_status' ran", message="Script execution: 'eventually_all_node_sync_status' ran")
sdk.unreachable(declare=True, id="No fault period: error fetching chainhead", message="No fault period: error fetching chainhead")
sdk.always(declare=True, id="No fault period: all nodes are synced (within 1)", message="No fault period: all nodes are synced (within 1)")

def all_node_sync_status():

    # waiting for a period of time for the system to recover. 
    time.sleep(30)

    all_nodes = nodes.get_all_nodes()
    node_height_dict = {f"{node}_height": None for node in all_nodes}

    for node in all_nodes:
        rpc_url, auth_token = nodes.get_url_and_token(node)
        
        chainhead = get_chainhead(node, rpc_url, auth_token)

        if not chainhead:
            sdk.unreachable(declare=False, id="No fault period: error fetching chainhead", message="No fault period: error fetching chainhead", condition=True, details={"node type":node,"chainhead":chainhead})
            return

        node_height_dict[f"{node}_height"] = chainhead['result']['Height']

    # each height is an int
    within_one = max(node_height_dict.values()) - min(node_height_dict.values()) <= 1

    if within_one:
        sdk.always(declare=False, id="No fault period: all nodes are synced (within 1)", message="No fault period: all nodes are synced (within 1)", condition=True, details=node_height_dict)
    else:
        sdk.always(declare=False, id="No fault period: all nodes are synced (within 1)", message="No fault period: all nodes are synced (within 1)", condition=False, details=node_height_dict)

    sdk.reachable(declare=False, id="Script execution: 'eventually_all_node_sync_status' ran", message="Script execution: 'eventually_all_node_sync_status' ran", condition=True)


all_node_sync_status()
