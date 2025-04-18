#!/bin/bash
LOTUS_1_TARGET=$(cat "/root/devgen/lotus-1/lotus-1-ipv4addr" 2>/dev/null || echo "")
LOTUS_2_TARGET=$(cat "/root/devgen/lotus-2/lotus-2-ipv4addr" 2>/dev/null || echo "")

random_targets=()
[[ -n "$LOTUS_1_TARGET" ]] && random_targets+=("$LOTUS_1_TARGET")
[[ -n "$LOTUS_2_TARGET" ]] && random_targets+=("$LOTUS_2_TARGET")

TARGET=${random_targets[$((RANDOM % ${#random_targets[@]}))]}

PING_ATTACK_TYPES=(
  "random"
  "oversized"
  "empty"
  "multiple"
  "incomplete"
  "barrage"
  "malformed"
  "connectdisconnect"
  "variable"
  "slow"
)
PING_ATTACK=${PING_ATTACK_TYPES[$((RANDOM % ${#PING_ATTACK_TYPES[@]}))]}

if [[ "$PING_ATTACK" == "barrage" || "$PING_ATTACK" == "connectdisconnect" ]]; then
  DURATION=$((RANDOM % 20 + 10))"s"  
elif [[ "$PING_ATTACK" == "slow" ]]; then
  DURATION=$((RANDOM % 60 + 60))"s"  
else
  DURATION=$((RANDOM % 40 + 20))"s" 
fi

if [[ "$PING_ATTACK" == "barrage" ]]; then
  CONCURRENCY=$((RANDOM % 20 + 10)) 
elif [[ "$PING_ATTACK" == "slow" || "$PING_ATTACK" == "variable" ]]; then
  CONCURRENCY=$((RANDOM % 5 + 1))    
else
  
  CONCURRENCY=$((RANDOM % 10 + 5))  
fi


if [[ "$PING_ATTACK" == "barrage" || "$PING_ATTACK" == "connectdisconnect" ]]; then
 
  MIN_INTERVAL="10ms"
  MAX_INTERVAL="50ms"
elif [[ "$PING_ATTACK" == "slow" ]]; then

  MIN_INTERVAL="200ms"
  MAX_INTERVAL="500ms"
else
  # Standard intervals
  MIN_INTERVAL="50ms"
  MAX_INTERVAL="200ms"
fi

echo "Running ping attack type '$PING_ATTACK' against $TARGET with concurrency $CONCURRENCY for $DURATION"
echo "Using intervals: $MIN_INTERVAL - $MAX_INTERVAL"

# Execute the ping attack
/opt/antithesis/app -operation ping \
  -target "$TARGET" \
  -ping-attack-type "$PING_ATTACK" \
  -concurrency "$CONCURRENCY" \
  -min-interval "$MIN_INTERVAL" \
  -max-interval "$MAX_INTERVAL" \
  -duration "$DURATION" || true

echo "Ping attack completed"
exit 0