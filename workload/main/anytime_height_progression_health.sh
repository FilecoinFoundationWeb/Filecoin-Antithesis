#!/bin/bash


echo "Running height progression monitoring for all nodes"

/opt/antithesis/app monitor height-progression --duration 2m --interval 10s --max-stalls 15
