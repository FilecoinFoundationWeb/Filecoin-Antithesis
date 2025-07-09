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

always("[+]" in lines[0], "[Forest] Node epoch is up to date during quiescence check", {"Response Text": lines})
always("[+]" in lines[1], "[Forest] Node rpc server is running during quiescence check", {"Response Text": lines})
always("[+]" in lines[2], "[Forest] Node is syncing during a quiscence check", {"Response Text": lines})
always("[+]" in lines[3], "[Forest] Node is connected to peers during a quiescence check", {"Response Text": lines})
always("[+]" in lines[4], "[Forest] Node has f3 running during a quiescence check", {"Response Text": lines})