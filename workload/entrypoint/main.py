import base64
import json
import os
import random
import string
import time
import requests

def generate_random_bytes(length):
    """Generates random bytes of a given length."""
    return bytes(random.getrandbits(8) for _ in range(length))

def generate_random_b64(min_len=32, max_len=128):
    """Generates a base64-encoded string of random bytes."""
    return base64.b64encode(generate_random_bytes(random.randint(min_len, max_len))).decode('utf-8')

def generate_cid_dict():
    """
    Generates a CID as a dictionary.
    A valid CID for testing: bafy2bzaceamp42wmmgr2g2ymg46euououzfyce7dlh4zwmi34ksf6xvwrimna
    """
    return {"/": "bafy2bzacec" + "".join(random.choices(string.ascii_lowercase + string.digits, k=50))}

def generate_address():
    """Generates a random Filecoin f0 address string."""
    return f"f0{random.randint(1000, 99999)}"

def generate_signature():
    """Generates a dictionary representing a Signature."""
    return {
        "Type": random.choice(["bls", "secp256k1"]),
        "Data": generate_random_b64()
    }

def generate_beacon_entry():
    """Generates a dictionary representing a BeaconEntry."""
    return {
        "Round": random.randint(1, 10000),
        "Data": generate_random_b64()
    }

def generate_post_proof():
    """Generates a dictionary representing a PoStProof."""
    return {
        "PoStProof": random.randint(0, 20), # Represents RegisteredPoStProof enum
        "ProofBytes": generate_random_b64()
    }

def get_base_block():
    """
    Returns a structurally valid GossipBlock dictionary.
    This block won't pass full consensus validation but should deserialize correctly
    and serve as a base for mutations.
    """
    return {
        "Header": {
            "Miner": generate_address(),
            "Parents": [generate_cid_dict()],
            "Weight": str(random.randint(1, 100000)),
            "Epoch": random.randint(1, 100000),
            "StateRoot": generate_cid_dict(),
            "MessageReceipts": generate_cid_dict(),
            "Messages": generate_cid_dict(),
            "BLSAggregate": generate_signature(),
            "Timestamp": int(time.time()),
            "BlockSig": generate_signature(),
            "VRFProof": generate_random_b64(32, 32),
            "ForkSignal": 0,
            "BeaconEntries": [generate_beacon_entry()],
            "WinPoStProof": [generate_post_proof()],
            "ParentBaseFee": "100",
        },
        "BlsMessages": [generate_cid_dict()],
        "SecpkMessages": [generate_cid_dict()]
    }

def get_fuzz_cases():
    """
    A generator of fuzzing cases. Each case is a function that
    takes a base block, mutates it, and returns it.
    """
    # Type confusion
    yield ("wrong_type_epoch", lambda b: mutate_field(b, ("Header", "Epoch"), "not_an_int"))
    yield ("wrong_type_weight", lambda b: mutate_field(b, ("Header", "Weight"), 12345)) # should be string
    yield ("wrong_type_parents", lambda b: mutate_field(b, ("Header", "Parents"), "not_an_array"))

    # Malformed data
    yield ("malformed_cid", lambda b: mutate_field(b, ("Header", "StateRoot"), {"bad": "cid"}))
    yield ("malformed_cid_string", lambda b: mutate_field(b, ("Header", "StateRoot"), "not_a_cid_object"))
    yield ("null_cid", lambda b: mutate_field(b, ("Header", "StateRoot"), None))
    yield ("malformed_signature_type", lambda b: mutate_field(b, ("Header", "BLSAggregate", "Type"), "invalid_sig_type"))
    yield ("malformed_signature_data", lambda b: mutate_field(b, ("Header", "BLSAggregate", "Data"), "not_b64"))

    # Missing fields
    yield ("missing_header", lambda b: remove_field(b, ("Header",)))
    yield ("missing_miner", lambda b: remove_field(b, ("Header", "Miner")))
    yield ("missing_parents", lambda b: remove_field(b, ("Header", "Parents")))
    yield ("missing_bls_messages", lambda b: remove_field(b, ("BlsMessages",)))

    # Boundary and size values
    yield ("huge_epoch", lambda b: mutate_field(b, ("Header", "Epoch"), 2**63 - 1))
    yield ("negative_epoch", lambda b: mutate_field(b, ("Header", "Epoch"), -100))
    yield ("very_large_weight", lambda b: mutate_field(b, ("Header", "Weight"), str(10**100)))
    yield ("many_parents", lambda b: mutate_field(b, ("Header", "Parents"), [generate_cid_dict() for _ in range(2000)]))
    yield ("many_bls_messages", lambda b: mutate_field(b, "BlsMessages", [generate_cid_dict() for _ in range(2000)]))
    yield ("large_vrf_proof", lambda b: mutate_field(b, ("Header", "VRFProof"), generate_random_b64(1024*1024, 1024*1024))) # 1MB
    yield ("empty_parents", lambda b: mutate_field(b, ("Header", "Parents"), []))
    yield ("empty_bls_messages", lambda b: mutate_field(b, "BlsMessages", []))
    
    # Completely empty/null block
    yield ("empty_block", lambda b: {})
    yield ("null_params", lambda b: None)

def mutate_field(block, path, new_value):
    """Mutates a field in a nested dictionary. Path is a tuple of keys."""
    d = block
    for key in path[:-1]:
        if key not in d:
            d[key] = {}
        d = d.get(key)
    d[path[-1]] = new_value
    return block

def remove_field(block, path):
    """Removes a field from a nested dictionary."""
    d = block
    for key in path[:-1]:
        d = d.get(key)
    if path[-1] in d:
        del d[path[-1]]
    return block

def send_rpc_request(url, token, block_payload):
    """Sends a SyncSubmitBlock request to the Forest node."""
    headers = {
        "Content-Type": "application/json",
    }
    if token:
        headers["Authorization"] = f"Bearer {token}"

    rpc_request = {
        "jsonrpc": "2.0",
        "method": "Filecoin.SyncSubmitBlock",
        "params": [block_payload],
        "id": 1
    }
    
    try:
        response = requests.post(url, json=rpc_request, headers=headers, timeout=10)
        return response.json()
    except requests.exceptions.RequestException as e:
        return {"error": str(e)}

def main():
    """Main fuzzing loop."""
    rpc_url = os.getenv("FOREST_RPC_URL", "http://10.20.20.28:3456/rpc/v1")
    
    token_path = "/root/devgen/forest/jwt"
    auth_token = None
    try:
        with open(token_path, 'r') as token_file:
            auth_token = token_file.read().strip()
    except IOError as e:
        print(f"Warning: Could not read token from {token_path}: {e}")

    print(f"Fuzzing Forest node at: {rpc_url}")
    if not auth_token:
        print(f"Warning: Forest auth token not found at {token_path}. Requests will be sent without auth.")

    for name, mutation_func in get_fuzz_cases():
        print(f"--- Running test case: {name} ---")
        base_block = get_base_block()
        fuzzed_block = mutation_func(base_block)

        # Pretty print the fuzzed block for inspection
        print("Payload:\n" + json.dumps(fuzzed_block, indent=2))
        
        response = send_rpc_request(rpc_url, auth_token, fuzzed_block)
        
        if "error" in response:
            print(f"Result: SUCCESS (Request rejected as expected)")
            print(f"  Error: {response['error']}")
        else:
            print(f"Result: FAILURE (Request was unexpectedly accepted)")
            print(f"  Response: {response}")
        
        time.sleep(0.1)

if __name__ == "__main__":
    main() 