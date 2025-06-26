#!/usr/bin/env -S python3 -u

from antithesis.assertions import (
    always,
    unreachable
)
import time
import requests

# forest node config
FOREST_IP = "10.20.20.28"
PORT = "2346"
ENDPOINT = "/healthz?verbose"
URL = f"http://{FOREST_IP}:{PORT}{ENDPOINT}"

# sleep to ensure the service is ready
print("Sleeping 20 seconds before starting health check...")
time.sleep(20)
print("Sleep completed!")

# checking that forest is reachable
try:
    response = requests.get(URL, timeout=5)
    response_text = response.text
except:
    unreachable("[Forest] Node is unreachable during quiescence period", None)
    exit(1)

lines = response_text.strip().split('\n')

failed_checks = any(
    line.startswith('[!]') for line in lines
)

always(not failed_checks, "[Forest] Node is healthy during quiescence check (Not checking F3)", {"Response Text": response_text})