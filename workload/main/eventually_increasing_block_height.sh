#!/bin/bash

echo "Running Test: IncreasingBlockHeight"
go test -v -count=1 /opt/antithesis/go-test-scripts/increasing_block_height_test.go
