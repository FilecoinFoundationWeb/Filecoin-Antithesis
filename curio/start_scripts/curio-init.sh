#!/usr/bin/env bash
echo CURIO_REPO_PATH="$CURIO_REPO_PATH"
echo Wait for lotus is ready ...
lotus wait-api
head=0
# Loop until the head is greater than 9
while [[ $head -le 9 ]]; do
  head=$(lotus chain list | awk '{print $1}' | awk -F':' '{print $1}' | tail -1)
  if [[ $head -le 9 ]]; then
    echo "Current head: $head, which is not greater than 9. Waiting..."
    sleep 1 # Wait for 4 seconds before checking again
  else
    echo "The head is now greater than 9: $head"
  fi
done

echo All ready. Lets go
myip=$(nslookup curio | grep -v "#" | grep Address | awk '{print $2}')

if [ ! -f "$CURIO_REPO_PATH"/.init.curio ]; then

  if [ ! -f "$CURIO_REPO_PATH"/.init.setup ]; then
    DEFAULT_WALLET=$(lotus wallet default)
    echo Create a new miner actor ...
    lotus-shed miner create "$DEFAULT_WALLET" "$DEFAULT_WALLET" "$DEFAULT_WALLET" 2KiB
    touch "$CURIO_REPO_PATH"/.init.setup
    lotus wallet export "$DEFAULT_WALLET" >"$CURIO_REPO_PATH"/default.key
  fi

  if [ ! -f "$CURIO_REPO_PATH"/.init.config ]; then
    newminer=$(lotus state list-miners | grep -E -v 't01000|t01001')
    echo "New Miner is $newminer"
    echo Initiating a new Curio cluster ...
    curio config new-cluster "$newminer"
    touch "$CURIO_REPO_PATH"/.init.config
  fi

  echo Starting Curio node to attach storage ...
  curio run --nosync --layers seal,post,gui &
  CURIO_PID=$!
  
  # Wait for the API to be ready with a timeout
  echo "Waiting for the curio API to become ready..."
  max_attempts=12
  attempt=1
  ready=false
  while [ $attempt -le $max_attempts ]; do
      if curio cli --machine "$myip":12300 wait-api >/dev/null 2>&1; then
          echo "Curio API is ready."
          ready=true
          break
      fi
      echo "Waiting for the curio API to become ready (attempt ${attempt}/${max_attempts})..."
      sleep 5
      attempt=$((attempt + 1))
  done

  if [ "$ready" = false ]; then
      echo "Curio API did not become ready in time. Exiting."
      kill -15 $CURIO_PID || kill -9 $CURIO_PID
      exit 1
  fi
  
  curio cli --machine "$myip":12300 log set-level --system chain --system chainxchg debug
  curio cli --machine $myip:12300 storage attach --init --seal --store --unseal $CURIO_REPO_PATH

  touch "$CURIO_REPO_PATH"/.init.curio
  echo Stopping Curio node ...
  echo Try to stop curio...
  kill -15 $CURIO_PID || kill -9 $CURIO_PID
  echo Done
fi

TOKEN=$(cat "$FOREST_DATA_DIR"/token)
FULLNODE_API_INFO=$TOKEN:/dns/forest/tcp/${FOREST_RPC_PORT}/http
export FULLNODE_API_INFO
lotus wallet import "$CURIO_REPO_PATH"/default.key || true
echo Starting curio node ...
exec curio run --nosync --layers seal,post,gui