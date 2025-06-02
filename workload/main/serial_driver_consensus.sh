#!/bin/bash

MAX_ATTEMPTS=5
ATTEMPT=1

while [ $ATTEMPT -le $MAX_ATTEMPTS ]; do
    echo "Attempt $ATTEMPT/$MAX_ATTEMPTS: Sending consensus fault"
    /opt/antithesis/app -operation sendConsensusFault
    
    if [ $? -eq 0 ]; then
        echo "Success!"
        exit 0
    fi
    
    echo "Attempt $ATTEMPT failed. Waiting 10 seconds before retry..."
    sleep 10
    ATTEMPT=$((ATTEMPT + 1))
done

echo "Failed after $MAX_ATTEMPTS attempts"
exit 1