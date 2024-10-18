
def lotus_rpc_get_auth_token(token_path:str) -> str:
    with open(token_path) as f:
        return f.read().rstrip()

def get_lotus_url_token():
    lotus_rpc_url = "http://10.20.20.24:1234/rpc/v0"
    BASE_PATH = "/root/devgen/lotus"
    LOTUS_TOKEN_TXT = "jwt"
    lotus_auth_token = lotus_rpc_get_auth_token(f'{BASE_PATH}/{LOTUS_TOKEN_TXT}')
    print(f"Workload [Lotus Auth Token]: Grabbed the lotus authentication token: '{lotus_auth_token}'")
    return lotus_rpc_url, lotus_auth_token
    