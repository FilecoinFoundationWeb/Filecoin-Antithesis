#!/usr/bin/env -S python3 -u

import time, sys
sys.path.append("/opt/antithesis/resources")
import nodes
from rpc import get_chainhead

from antithesis.assertions import (
    always,
)

def check_increasing_block_height(n=3, time_limit=7.5):

    node = nodes.select_random_node()
    rpc_url, auth_token = nodes.get_url_and_token(node)

    for i in range(n):
        chainhead = get_chainhead(node, rpc_url, auth_token)
        if not chainhead:
            return
        height = chainhead['result']['Height']
        s = time.monotonic_ns()
        while True:
            chainhead = get_chainhead(node, rpc_url, auth_token)
            if not chainhead:
                return
            new_height = chainhead['result']['Height']
            if height + 1 == new_height:
                e = time.monotonic_ns()
                diff = round((s - e)/1_000_000_000, 2)
                print(f"Workload [main][anytime_increasing_block_height_check.py]: time difference between block height {height} and block height {new_height} was {diff} seconds")
                if node == "forest":
                    always(diff < time_limit, "Forest: Chainhead increases within the time boundary (7.5s)", {"old height":height,"new height":new_height,"time difference":diff})
                if node == "lotus":
                    always(diff < time_limit, "Lotus: Chainhead increases within the time boundary (7.5s)", {"old height":height,"new height":new_height,"time difference":diff})
                break
            time.sleep(0.5)

check_increasing_block_height()
