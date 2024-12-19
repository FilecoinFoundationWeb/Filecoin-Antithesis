#!/bin/bash

echo "Running Test: All F3 API calls"
go test -v -count=1 /opt/antithesis/test/v1/main/f3_consistent_pt_test.go
