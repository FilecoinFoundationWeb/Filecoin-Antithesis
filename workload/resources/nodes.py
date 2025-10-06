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
        rpc_url = "http://forest0:3456/rpc/v0"
        base_path = "/root/devgen/forest"
        token_txt = "jwt"
    elif node_type == "lotus-1":
        rpc_url = "http://lotus0:1234/rpc/v1"
        base_path = "/root/devgen/lotus-1"
        token_txt = "jwt"
    elif node_type == "lotus-2":
        rpc_url = "http://lotus1:1235/rpc/v1"
        base_path = "/root/devgen/lotus-2"
        token_txt = "jwt"
    else:
        unreachable("Invalid node for getting authentication credentials", {"invalid_node":node_type})
        return None, None
    auth_token = get_token(f'{base_path}/{token_txt}')
    print(f"Workload [rpc_url_and_auth_token.py]: Got the {node_type} authentication token: '{auth_token}'")
    reachable("Got an authentication token for a node", {"node":node_type,"rpc_url":rpc_url,"auth_token":auth_token})
    return rpc_url, auth_token

def select_random_node():
    nodes = ["forest"]
    return random.choice(nodes)

def get_all_nodes():
    nodes = ["forest", "lotus-1", "lotus-2"]
    return nodes
