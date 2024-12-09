#!/bin/bash

echo "Running Test: F3IsRunning"
go test -v -count=1 /opt/antithesis/test/v1/main/f3_running_test.go
