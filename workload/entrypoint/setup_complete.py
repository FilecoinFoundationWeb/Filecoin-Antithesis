#!/usr/bin/env -S python3 -u

from antithesis.lifecycle import (
    setup_complete,
)

setup_complete({"Message":"Lotus-1 has reached blockheight 10"})
print("system is healthy")