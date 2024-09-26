#!/bin/bash
sleep 90
echo $(curl 10.20.20.21/info)
curl 10.20.20.21/info | jq -c > /opt/forest/start_scripts/chain_info
export DRAND_CHAIN_INFO=/opt/forest/start_scripts/chain_info
while [ ! -f /opt/forest/start_scripts/chain_info ]; do
  echo "Waiting for chain_info file..."
  sleep 5
done


ls -l /opt/forest/start_scripts/shared/
NETWORK_NAME=$(cat /opt/forest/start_scripts/shared/network_name)
echo "The network name is: $NETWORK_NAME"

forest-tool api serve --chain $NETWORK_NAME --genesis /opt/forest/start_scripts/shared/devgen.car --port 2345
forest-cli sync status

