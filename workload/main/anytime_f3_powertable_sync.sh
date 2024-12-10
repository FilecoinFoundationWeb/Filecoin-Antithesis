#!/bin/bash

echo "Running Test: F3GetF3PowerTable"
go test -v -count=1 /opt/antithesis/test/v1/main/f3_powertable_sync_test.go
