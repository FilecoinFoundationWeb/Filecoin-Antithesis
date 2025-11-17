#!/bin/bash

export FILECOIN_NODES="http://lotus-1:1234/rpc/v1,http://lotus-2:1235/rpc/v1,http://forest:3456/rpc/v1"
filwizard properties --check state-consistency

