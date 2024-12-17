#!/bin/bash

echo "Running Test: F3GetCertificate"
go test -v -count=1 /opt/antithesis/test/v1/main/f3_get_certificate_test.go
