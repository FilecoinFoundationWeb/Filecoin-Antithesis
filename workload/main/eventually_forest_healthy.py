#!/usr/bin/env -S python3 -u

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

# curl healthcheck
try:
    response = requests.get(URL, timeout=5)
    response_text = response.text
except requests.RequestException:
    response_text = "No response from Forest healthcheck call"
    always(False, "Forest node stays reachable", response_text)
    exit(1)
# if we have [!] it means something is unhealthy
if "[!]" in response_text:
    if "f3 not running" and "epoch up to date" and "rpc server running" and "sync ok" and "peers connected" in response_text:
        print("Disabled F3 check for Forest")
    else:
        always(False, "Forest node stays healthy", response_text)
        exit(1)
else:
    print("Forest node is healthy:", response_text)
