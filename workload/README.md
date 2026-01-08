# Filecoin Antithesis Workload

Modular testing workload for Filecoin nodes with Antithesis integration.

## Structure

```
workload/
├── resources/              # Shared utilities
│   ├── config.go           # Config loading, Node struct
│   ├── client.go           # API client connections
│   └── assert.go           # Antithesis assertion wrappers
├── properties/             # Invariant checkers
│   ├── finalization/       # Finalized tipset consistency
│   ├── chain/              # Height monotonicity
│   ├── state/              # State root determinism
│   └── liveness/           # Chain progress
├── drivers/                # Workloads (mempool, network, etc.)
└── main.go                 # CLI entry point
```

## Config

Create `/opt/antithesis/resources/config.json`:

```json
{
  "nodes": [
    {"id": "lotus0", "rpc": "http://lotus0:1234/rpc/v1", "token": "/root/devgen/lotus0/jwt", "implementation": "lotus", "role": "full"},
    {"id": "lotus1", "rpc": "http://lotus1:1234/rpc/v1", "token": "/root/devgen/lotus1/jwt", "implementation": "lotus", "role": "full"},
    {"id": "forest0", "rpc": "http://forest0:2345/rpc/v1", "token": "", "implementation": "forest", "role": "full"}
  ]
}
```

## Properties

| Property | Type | Description |
|----------|------|-------------|
| `finalized_tipsets_match` | Always | All nodes agree on finalized tipset CIDs |
| `chain_height_never_decreases` | Always | Chain head height is monotonic per node |
| `state_root_deterministic` | Always | Same state root at finalized heights |
| `chain_progresses` | Sometimes | Chain height increases over time |

## Usage

```bash
# Run all property checks once
workload property check-all

# Check specific property
workload property check-finalization
workload property check-height
workload property check-state
workload property check-liveness --window 60s

# Continuous monitoring
workload property monitor --interval 30s
```

## Antithesis Integration

Properties use Antithesis SDK assertions:
- `Always()` - Must hold every check (safety)
- `Sometimes()` - Must hold at least once (liveness)
