#!/usr/bin/env -S python3 -u

import random, sys
sys.path.append("/opt/antithesis/sdk")
from antithesis_sdk import antithesis_fallback_sdk

sdk = antithesis_fallback_sdk()
sdk.reachable(declare=True, id="Grabbed an authentication token", message="Successfully grabbed an authentication token for a node")
sdk.unreachable(declare=True, id="Invalid node type for authentication", message="Invalid node type for get_url_and_token method")

def get_token(token_path:str) -> str:
    with open(token_path) as f:
        return f.read().rstrip()

def get_url_and_token(node_type:str):
    if node_type == "forest":
        rpc_url = "http://10.20.20.26:3456/rpc/v0"
        base_path = "/root/devgen/forest"
        token_txt = "token.jwt"
    elif node_type == "lotus":
        rpc_url = "http://10.20.20.24:1234/rpc/v0"
        base_path = "/root/devgen/lotus"
        token_txt = "jwt"
    else:
        sdk.unreachable(declare=False, id="Invalid node type for authentication", message="Invalid node type for get_url_and_token method", condition=True, details={"passed through node":node_type})
        return None, None
    auth_token = get_token(f'{base_path}/{token_txt}')
    print(f"Workload [rpc_url_and_auth_token.py]: Grabbed the {node_type} authentication token: '{auth_token}'")
    sdk.reachable(declare=False, id="Grabbed an authentication token", message="Successfully grabbed an authentication token for a node", condition=True, details={"rpc url and auth token:":[rpc_url, auth_token]})
    return rpc_url, auth_token

def select_random_node():
    nodes = ["forest","lotus"]
    return random.choice(nodes)

def get_all_nodes():
    nodes = ["forest","lotus"]
    return nodes
