#!/usr/bin/env -S python3 -u

import requests
import time

from antithesis.assertions import (
    reachable,
    unreachable,
)

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

    max_retries = 5
    wait_seconds = 1

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

        for attempt in range(max_retries):
            try:
                response = func(rpc_url, headers=headers, **kwargs)
                break
            except Exception as e:
                print(f"Attempt {attempt + 1} failed: {e}")
                if attempt < max_retries - 1:
                    time.sleep(wait_seconds)
                else:
                    raise
        
        reachable("A RPC request was send and a response was received", None)

        return {
            'request': {
                'url': rpc_url,
                'headers': headers,
                'payload': payload,
            },
            'response': response
        }
    
    print(f"Workload [request.py]: No request was sent because method was {method}")
    unreachable("Invalid HTTP method in a RPC request", {"invalid http method":method})
    return None