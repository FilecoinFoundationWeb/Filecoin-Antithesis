#!/bin/bash

echo "Running Test: All F3 API calls"
go test -v -count=1 /opt/antithesis/go-test-scripts/f3_consistent_pt_test.go
