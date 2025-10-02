#!/bin/bash

retries=10
while [ "$retries" -gt 0 ]; do
    echo "-------------------"
    retries=$(( retries - 1 ))
    echo "lotus${no}: $retries connection attempts remaining..."
    echo "-------------------"
done
