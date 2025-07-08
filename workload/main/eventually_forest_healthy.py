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

for line in lines:
    passing_check = line.startswith('[+]')
    if "epoch up to date" in line:
        print(passing_check)
        print(line)
        always(passing_check, "[Forest] Node epoch is up to date during quiescence check", {"Response Text": line})
    elif "rpc server running" in line:
        print(passing_check)
        print(line)
        always(passing_check, "[Forest] Node rpc server is running during quiescence check", {"Response Text": line})
    elif "sync ok" in line:
        print(passing_check)
        print(line)
        always(passing_check, "[Forest] Node is syncing during a quiscence check", {"Response Text": line})
    elif "peers connected" in line:
        print(passing_check)
        print(line)
        always(passing_check, "[Forest] Node is connected to peers during a quiescence check", {"Response Text": line})
    elif "f3 not running" in line:
        print(passing_check)
        print(line)
        always(passing_check, "[Forest] Node has f3 running during a quiescence check", {"Response Text": line})
