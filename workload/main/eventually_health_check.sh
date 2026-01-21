#!/bin/bash

WORKLOAD="/opt/antithesis/workload"

echo "Running Workload: Chain Backfill"
$WORKLOAD chain backfill

echo "Running comprehensive health check (includes peer count, F3 status)"
$WORKLOAD monitor comprehensive --monitor-duration 30s

echo "Running ETH methods consistency check"
$WORKLOAD eth check

echo "Running consensus check"
$WORKLOAD consensus check
