#!/bin/bash

echo "Running Test: F3GetCertificateEquality"
go test -v -count=1 /opt/antithesis/test/v1/main/f3_get_equal_progress_test.go
