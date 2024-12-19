#!/bin/bash

echo "Running Test: IncreasingBlockHeight"
go test -v -count=1 /opt/antithesis/test/v1/main/increasing_block_height_test.go
