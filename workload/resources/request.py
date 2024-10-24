import requests

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

        # print(f"Workload [request.py]: request response was {response}")

        return {
            'request': {
                'url': rpc_url,
                'headers': headers,
                'payload': payload,
            },
            'response': response
        }
    
    print(f"Workload [request.py]: No request was sent because method was {method}")
    return None