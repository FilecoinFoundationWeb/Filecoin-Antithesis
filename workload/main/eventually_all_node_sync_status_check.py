#!/usr/bin/env -S python3 -u

import time, sys
sys.path.append("/opt/antithesis/resources")
sys.path.append("/opt/antithesis/sdk")
import nodes
from rpc import get_chainhead
from antithesis_sdk import antithesis_fallback_sdk


sdk = antithesis_fallback_sdk()

sdk.reachable(declare=True, id="Successful 'eventually_all_node_sync_status.py' script execution", message="Successful 'eventually_all_node_sync_status.py' script execution")
sdk.unreachable(declare=True, id="Error fetching chainhead during quiescent period", message="Error fetching chainhead during quiescent period")
sdk.always(declare=True, id="Quiescent Period: nodes should be equally synced", message="Quiescent Period: nodes should be equally synced")

def all_node_sync_status():

    # waiting for a period of time for the system to recover. 
    time.sleep(30)

    all_nodes = nodes.get_all_nodes()
    node_heights = []

    for node in all_nodes:
        rpc_url, auth_token = nodes.get_url_and_token(node)
        
        chainhead = get_chainhead(node, rpc_url, auth_token)

        if not chainhead:
            sdk.unreachable(declare=False, id="Error fetching chainhead during quiescent period", message="Error fetching chainhead during quiescent period", condition=True, details={"chainhead":chainhead})
            return

        node_heights.append(chainhead['result']['Height'])

    # each height is an int
    all_equal = all(height == node_heights[0] for height in node_heights)

    if all_equal:
        sdk.always(declare=False, id="Quiescent Period: nodes should be equally synced", message="Quiescent Period: nodes are all equally synced", condition=True, details={"node_heights":node_heights})
    else:
        sdk.always(declare=False, id="Quiescent Period: nodes should be equally synced", message="Quiescent Period: nodes are not equally synced", condition=False, details={"node_heights":node_heights})

    sdk.reachable(declare=False, id="Successful 'eventually_all_node_sync_status.py' script execution", message="Successful 'eventually_all_node_sync_status.py' script execution", condition=True)


all_node_sync_status()
