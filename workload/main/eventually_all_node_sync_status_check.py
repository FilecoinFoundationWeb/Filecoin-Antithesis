#!/usr/bin/env -S python3 -u

import time, sys
sys.path.append("/opt/antithesis/resources")
import nodes
from rpc import get_chainhead

from antithesis.assertions import (
    always,
)

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

    always(within_one, "All nodes are synced (within 1) during period of no faults", node_height_dict)

all_node_sync_status()
