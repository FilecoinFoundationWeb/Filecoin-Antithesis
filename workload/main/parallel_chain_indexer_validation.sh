#!/bin/bash

echo "Running Test: Validating Chain Index"
go test -v -count=1 /opt/antithesis/go-test-scripts/chain_indexer_test.go
