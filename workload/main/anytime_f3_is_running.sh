#!/bin/bash

echo "Running Test: F3IsRunning"
go test -v -count=1 /opt/antithesis/go-test-scripts/f3_running_test.go
