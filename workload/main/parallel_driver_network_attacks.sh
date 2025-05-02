#!/bin/bash
LOTUS_1_TARGET=$(cat "/root/devgen/lotus-1/lotus-1-ipv4addr" 2>/dev/null || echo "")
LOTUS_2_TARGET=$(cat "/root/devgen/lotus-2/lotus-2-ipv4addr" 2>/dev/null || echo "")

random_targets=()
[[ -n "$LOTUS_1_TARGET" ]] && random_targets+=("$LOTUS_1_TARGET")
[[ -n "$LOTUS_2_TARGET" ]] && random_targets+=("$LOTUS_2_TARGET")

TARGET=${random_targets[$((RANDOM % ${#random_targets[@]}))]}

DURATION=$((RANDOM % 4 + 1))"m"

ATTACK_CATEGORIES=("chaos" "identify" "ping" "ping" "ping")
ATTACK_CATEGORY=${ATTACK_CATEGORIES[$((RANDOM % ${#ATTACK_CATEGORIES[@]}))]}

if [[ "$ATTACK_CATEGORY" == "ping" ]]; then
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
    ATTACK_TYPE="ping-${PING_ATTACK}"
else
    ATTACK_TYPE=$ATTACK_CATEGORY
fi

echo "Running $ATTACK_TYPE attack against $TARGET for $DURATION"

# Run the appropriate attack
if [[ "$ATTACK_TYPE" == "chaos" ]]; then
    # Chaos operation with random intervals
    MIN_INTERVAL=$((RANDOM % 5 + 1))"s"
    MAX_INTERVAL=$((RANDOM % 15 + 5))"s"
    
    /opt/antithesis/app -operation chaos \
      -target "$TARGET" \
      -min-interval "$MIN_INTERVAL" \
      -max-interval "$MAX_INTERVAL" \
      -duration "$DURATION" || true
    
elif [[ "$ATTACK_TYPE" == "identify" ]]; then
    export LOTUS_TARGET="$TARGET"
    go run /opt/antithesis/go-test-scripts/identify.go || true
    
elif [[ "$ATTACK_TYPE" =~ ^ping- ]]; then
    PING_ATTACK="${ATTACK_TYPE#ping-}"
    if [[ "$PING_ATTACK" == "barrage" || "$PING_ATTACK" == "connectdisconnect" ]]; then
        CONCURRENCY=$((RANDOM % 15 + 10))  
        MIN_INTERVAL="10ms"
        MAX_INTERVAL="50ms"
    elif [[ "$PING_ATTACK" == "slow" || "$PING_ATTACK" == "variable" ]]; then
       
        CONCURRENCY=$((RANDOM % 3 + 1))   
        MIN_INTERVAL="100ms" 
        MAX_INTERVAL="300ms"
    else
        CONCURRENCY=$((RANDOM % 8 + 2))    
        MIN_INTERVAL="50ms"
        MAX_INTERVAL="200ms"
    fi
    
    echo "Using ping attack type: $PING_ATTACK with concurrency: $CONCURRENCY"
    /opt/antithesis/app -operation pingAttack \
      -target "$TARGET" \
      -ping-attack-type "$PING_ATTACK" \
      -concurrency "$CONCURRENCY" \
      -min-interval "$MIN_INTERVAL" \
      -max-interval "$MAX_INTERVAL" \
      -duration "$DURATION" || true
fi

echo "Attack completed"
# Always exit with success code
exit 0
