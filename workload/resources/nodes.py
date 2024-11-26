#!/usr/bin/env -S python3 -u

import random, sys
sys.path.append("/opt/antithesis/sdk")
from antithesis_sdk import antithesis_fallback_sdk

sdk = antithesis_fallback_sdk()
sdk.reachable(declare=True, id="Got an authentication token for a node", message="Got an authentication token for a node")
sdk.unreachable(declare=True, id="Invalid node for getting authentication credentials", message="Invalid node for getting authentication credentials")

def get_token(token_path:str) -> str:
    with open(token_path) as f:
        return f.read().rstrip()

def get_url_and_token(node_type:str):
    if node_type == "forest":
        rpc_url = "http://10.20.20.28:3456/rpc/v0"
        base_path = "/root/devgen/forest"
        token_txt = "jwt"
    elif node_type == "lotus-1":
        rpc_url = "http://10.20.20.24:1234/rpc/v0"
        base_path = "/root/devgen/lotus-1"
        token_txt = "jwt"
    else:
        sdk.unreachable(declare=False, id="Invalid node for getting authentication credentials", message="Invalid node for getting authentication credentials", condition=True, details={"invalid node":node_type})
        return None, None
    auth_token = get_token(f'{base_path}/{token_txt}')
    print(f"Workload [rpc_url_and_auth_token.py]: Got the {node_type} authentication token: '{auth_token}'")
    sdk.reachable(declare=False, id="Got an authentication token for a node", message="Got an authentication token for a node", condition=True, details={"node":node_type,"rpc_url":rpc_url,"auth_token":auth_token})
    return rpc_url, auth_token

def select_random_node():
    nodes = ["forest"]
    return random.choice(nodes)

def get_all_nodes():
    nodes = ["forest"]
    return nodes
