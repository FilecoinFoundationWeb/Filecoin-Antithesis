#!/usr/bin/env -S python3 -u

import sys
sys.path.append("/opt/antithesis/sdk")

from antithesis_sdk import antithesis_fallback_sdk
import requests

sdk = antithesis_fallback_sdk()
sdk.unreachable(declare=True, id="Invalid HTTP method in a RPC request", message="Invalid HTTP method in a RPC request")
sdk.reachable(declare=True, id="A RPC request was send and a response was received", message="A RPC request was send and a response was received")

def request(node_type:str, rpc_url:str, auth_token:str, method:str, payload:dict) -> dict:
    '''
    @purpose - making raw api requests
    @param method - get | post
    @param payload - request payload
    @param rpc_url - node http address
    @param auth_token - node authentication token
    @return - dictionary with request and response information
    '''

    print(f"Workload [request.py]: executing a request on a {node_type} node")

    headers = {
        "Content-Type": "application/json",
        "Authorization": f'Bearer {auth_token}'
    }

    if method in ['get', 'post', 'put', 'delete', 'head', 'options']:

        # @todo: need to provide stuffing of additional kwargs
        # payloads are mapped differently in the request call
        payload_mapping = {
            'get': 'params',
            'post': 'data',
        }

        kwargs = {}

        if bool(payload):
            if method in payload_mapping.keys():
                kwargs.update({payload_mapping[method]: payload})

        func = getattr(requests, method)
        response = func(rpc_url, headers=headers, **kwargs)
        sdk.reachable(declare=False, id="A RPC request was send and a response was received", message="A RPC request was send and a response was received", condition=True)

        return {
            'request': {
                'url': rpc_url,
                'headers': headers,
                'payload': payload,
            },
            'response': response
        }
    
    print(f"Workload [request.py]: No request was sent because method was {method}")
    sdk.unreachable(declare=False, id="Invalid HTTP method in a RPC request", message="Invalid HTTP method in a RPC request", condition=True, details={"invalid http method":method})
    return None