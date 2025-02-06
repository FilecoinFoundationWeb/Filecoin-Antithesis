#!/usr/bin/env -S python3 -u

import random

from antithesis.assertions import (
    reachable,
    unreachable,
)

def get_token(token_path:str) -> str:
    with open(token_path) as f:
        return f.read().rstrip()

def get_url_and_token(node_type:str):
    if node_type == "forest":
        rpc_url = "http://10.20.20.28:3456/rpc/v0"
        base_path = "/root/devgen/forest"
        token_txt = "jwt"
        auth_token = get_token(f'{base_path}/{token_txt}')
        reachable("Forest: Got an authentication token", {"node":node_type,"rpc_url":rpc_url,"auth_token":auth_token})
    elif node_type == "lotus":
        rpc_url = "http://10.20.20.24:1234/rpc/v0"
        base_path = "/root/devgen/lotus-1"
        token_txt = "jwt"
        auth_token = get_token(f'{base_path}/{token_txt}')
        reachable("Lotus: Got an authentication token", {"node":node_type,"rpc_url":rpc_url,"auth_token":auth_token})
    else:
        unreachable("Lotus: Invalid node for getting authentication credentials", {"invalid_node":node_type})
        unreachable("Forest: Invalid node for getting authentication credentials", {"invalid_node":node_type})
        return None, None
    print(f"Workload [rpc_url_and_auth_token.py]: Got the {node_type} authentication token: '{auth_token}'")
    return rpc_url, auth_token

def select_random_node():
    nodes = ["forest", "lotus"]
    return random.choice(nodes)

def get_all_nodes():
    nodes = ["forest", "lotus"]
    return nodes
