#!/bin/bash


cd /opt/antithesis/test/v1/main

# Run the fuzz test
echo "Running Fuzz Test: FuzzBuildAndSignMessages"
go test -tags=fuzzing -run=^$ -fuzz=FuzzBuildAndSignMessages -fuzztime=30s
