#!/usr/bin/env bash
set -e
echo CURIO_REPO_PATH=$CURIO_REPO_PATH
export LOTUS_PATH=${LOTUS_1_PATH}
apt install xxd jq -y
echo Wait for lotus is ready ...
lotus wait-api
head=0
# Loop until the head is greater than 9
while [[ $head -le 9 ]]; do
    head=$(lotus chain list | awk '{print $1}' | awk -F':' '{print $1}' | tail -1)
    if [[ $head -le 9 ]]; then
        echo "Current head: $head, which is not greater than 9. Waiting..."
        sleep 1  # Wait for 4 seconds before checking again
    else
        echo "The head is now greater than 9: $head"
    fi
done

echo All ready. Lets go
myip=`nslookup curio | grep -v "#" | grep Address | awk '{print $2}'`

if [ ! -f "$CURIO_REPO_PATH"/.init.curio ]; then

  if [ ! -f "$CURIO_REPO_PATH"/.init.setup ]; then
    DEFAULT_WALLET=$(lotus wallet default)
    echo Create a new miner actor ...
	sptool --actor t01000 actor new-miner --owner $DEFAULT_WALLET --worker $DEFAULT_WALLET --from $DEFAULT_WALLET --sector-size 2KiB --confidence 1
    touch "$CURIO_REPO_PATH"/.init.setup
    lotus wallet export "$DEFAULT_WALLET" >"$CURIO_REPO_PATH"/default.key
  fi

  if [ ! -f "$CURIO_REPO_PATH"/.init.config ]; then
    newminer=`lotus state list-miners | grep -E -v 't01000|t01001'`
    echo "New Miner is $newminer"
    echo Initiating a new Curio cluster ...
    curio config new-cluster "$newminer"
    touch "$CURIO_REPO_PATH"/.init.config
  fi

  echo Starting Curio node to attach storage ...
  curio run --nosync --layers seal,post,gui &
  CURIO_PID=$!
  sleep 60
  until curio cli --machine "$myip":12300 wait-api; do
    echo "Waiting for the curio CLI to become ready..."
    sleep 5
  done
  curio cli --machine "$myip":12300 storage attach --init --seal --store "$CURIO_REPO_PATH"
  touch "$CURIO_REPO_PATH"/.init.curio
  echo Stopping Curio node ...
  echo Try to stop curio...
  kill -15 $CURIO_PID || kill -9 $CURIO_PID
  echo Done
fi
if [ -f "$CURIO_REPO_PATH"/default.key ]; then
    echo "Importing wallet key..."
    lotus wallet import "$CURIO_REPO_PATH"/default.key || true
fi

echo Starting curio node ...
curio run --nosync --name devnet --layers seal,post,gui &
CURIO_PID=$!

# Wait a moment for curio to start
sleep 10

# Get the miner ID if not already set
if [ -z "$newminer" ]; then
    newminer=$(lotus state list-miners | grep -E -v 't01000|t01001' | head -1)
    echo "Using miner: $newminer"
fi

# Start sealing
echo "Starting sealing process..."
curio seal start --now --cc --actor "$newminer"

# Keep the script running
wait $CURIO_PID


