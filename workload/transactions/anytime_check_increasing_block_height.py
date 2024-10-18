import time, sys
sys.path.append("/opt/antithesis/resources")

from lotus_rpc_token import get_lotus_url_token
from rpc import get_chainhead


# I think this is not a great validation check... FIX!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!

def check_increasing_block_height():
    lotus_rpc_url, lotus_auth_token = get_lotus_url_token()

    chainhead = get_chainhead(lotus_rpc_url, lotus_auth_token)

    if not chainhead:
        return 

    height_1 = chainhead['result']['Height']

    time.sleep(30)

    chainhead = get_chainhead(lotus_rpc_url, lotus_auth_token)

    if not chainhead:
        return

    height_2 = chainhead['result']['Height']

    if height_1 + 1 == height_2:
        print("Worklaod [check_increasing_block_height.py]: lotus block height is increasing as expected")
        return
    
    print("Worklaod [check_increasing_block_height.py]: lotus block height is not increasing as expected")



check_increasing_block_height()

