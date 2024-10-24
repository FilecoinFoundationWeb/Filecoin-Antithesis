
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
        print("Workload [rpc_url_and_auth_token.py]: Error: Invalid node type")
        return None, None
    auth_token = get_token(f'{base_path}/{token_txt}')
    print(f"Workload [rpc_url_and_auth_token.py]: Grabbed the {node_type} authentication token: '{auth_token}'")
    return rpc_url, auth_token
