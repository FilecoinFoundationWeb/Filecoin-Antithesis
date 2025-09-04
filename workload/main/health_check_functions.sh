#!/bin/bash

# Health check functions for node crash fault tolerance
# Source this file in all scripts that interact with nodes

# Check if all required nodes are running
check_all_nodes_running() {
    local nodes=("lotus-1" "lotus-2" "forest")
    
    for node in "${nodes[@]}"; do
        if ! check_node_running "$node"; then
            log_node_fault "$node"
            return 1
        fi
    done
    
    return 0
}

# Log when a node is detected as down
log_node_fault() {
    local node="$1"
    echo "$(date): [$(basename $0)] Node $node is not running (may be under crash/restart fault)"
}

# Exit gracefully if any nodes are down
exit_if_nodes_down() {
    if ! check_all_nodes_running; then
        echo "$(date): [$(basename $0)] One or more nodes are under crash/restart fault. Exiting gracefully."
        exit 0  # Exit with success, not error
    fi
}

check_node_running() {
    local node="$1"
    
    case "$node" in
        "lotus-1")
            endpoint="http://lotus-1:1234"
            ;;
        "lotus-2")
            endpoint="http://lotus-2:1235"
            ;;
        "forest")
            endpoint="http://forest:3456"
            ;;
        *)
            return 1
            ;;
    esac
    
    # Test RPC connectivity with a simple ChainHead call
    local response
    response=$(curl -s -X POST "$endpoint/rpc/v1" \
        -H "Content-Type: application/json" \
        -d '{"jsonrpc":"2.0","method":"Filecoin.ChainHead","params":[],"id":1}' \
        --max-time 5 2>/dev/null)
    
    # Check if we got a valid JSON response
    if echo "$response" | jq -e '.result' >/dev/null 2>&1; then
        return 0
    else
        return 1
    fi
}

# Log script start with health check
log_script_start() {
    echo "$(date): [$(basename $0)] Starting with health check..."
    exit_if_nodes_down
    echo "$(date): [$(basename $0)] All nodes healthy, proceeding..."
} 

# Return the PID file path used to indicate a running reorg singleton
reorg_pid_file() {
    echo "/tmp/singleton_driver_reorg.pid"
}

# Check if the reorg singleton is currently running
is_reorg_running() {
    local pid_file
    pid_file="$(reorg_pid_file)"

    if [ -f "$pid_file" ]; then
        local pid
        pid="$(cat "$pid_file" 2>/dev/null)"
        if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
            return 0
        fi
    fi

    return 1
}

# Exit early (success) if reorg singleton is running
exit_if_reorg_running() {
    if is_reorg_running; then
        echo "$(date): [$(basename $0)] Reorg singleton is running; skipping."
        exit 0
    fi
}