#!/bin/bash

WORKLOAD="/opt/antithesis/workload"

echo "Running comprehensive node health monitoring with all features enabled"

# Run comprehensive check with extended duration for better monitoring
$WORKLOAD monitor comprehensive \
    --chain-notify \
    --height-progression \
    --peer-count \
    --f3-status \
    --monitor-duration 2m \
    --height-check-interval 10s \
    --max-consecutive-stalls 4
