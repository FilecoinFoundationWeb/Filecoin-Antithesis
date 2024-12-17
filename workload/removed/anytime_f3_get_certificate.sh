#!/bin/bash

echo "Running Test: F3GetLatestcertificate"
go test -v -count=1 /opt/antithesis/test/v1/main/f3_get_latest_cert_test.go
