#!/bin/bash

set -euxo pipefail

/opt/antithesis/app stress messages \
    --node Lotus1 &

/opt/antithesis/app stress messages \
    --node Lotus2 &

wait