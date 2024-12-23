#!/bin/bash

echo "Running Test: FuzzBuildAndSignMessages"
go test -tags=fuzzing -run=^$ -fuzz=FuzzBuildAndSignMessages -fuzztime=30m
