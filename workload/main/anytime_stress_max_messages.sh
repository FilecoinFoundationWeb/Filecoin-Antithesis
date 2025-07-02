#!/bin/bash

set -euxo pipefail

/opt/antithesis/app \
    --operation stressMaxMessages \
    --node Lotus1 &

/opt/antithesis/app \
    --operation stressMaxMessages \
    --node Lotus2 &

wait