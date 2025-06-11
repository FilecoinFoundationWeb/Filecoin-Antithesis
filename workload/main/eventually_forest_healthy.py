import os
import time
import requests
from antithesis.assertions import always

# Forest node config
FOREST_IP = "10.20.20.28"
PORT = "2346"
ENDPOINT = "/healthz?verbose"
URL = f"http://{FOREST_IP}:{PORT}{ENDPOINT}"

# Sleep to ensure the service is ready
time.sleep(20)

try:
    response = requests.get(URL, timeout=5)
    response_text = response.text
except requests.RequestException:
    response_text = ""
    always(False, "Forest node stays reachable")
    exit(1)

if "[!]" in response_text:
    always(False, "Forest node stays healthy")
    exit(1)
else:
    print("Forest node is healthy:", response_text)
