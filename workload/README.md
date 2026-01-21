# Filecoin Antithesis Workload

This directory contains the test workload for validating Filecoin nodes (Lotus, Forest) using the Antithesis testing platform.

## CLI Binary

The workload binary is located at `/opt/antithesis/workload` inside the container.

```bash
docker exec workload /opt/antithesis/workload <command>
```

## Available Commands

### Wallet Management
```bash
wallet create --node Lotus0 --count 5   # Create and fund wallets
```

### Network Operations
```bash
network connect --node Lotus0           # Connect to peers
network disconnect --node Lotus0        # Disconnect from peers
network reorg --node Lotus0             # Simulate reorg
```

### Mempool Operations
```bash
mempool track --node Lotus0 --duration 5m --interval 5s   # Track mempool
mempool spam                                               # Spam transactions
```

### Chain Operations
```bash
chain backfill         # Check chain index backfill (Lotus only)
chain common-tipset    # Get common finalized tipset across all nodes
```

### State Operations
```bash
state check --node Lotus0           # Check state on single node
state compare --epochs 10           # Compare state across all nodes
state compare-at-height --height N  # Compare at specific height
```

### Consensus
```bash
consensus check                     # Check tipset consensus
```

### Monitoring
```bash
monitor height-progression --duration 1m --interval 5s   # Track height changes
monitor comprehensive                                     # Full health check
```

### ETH Compatibility
```bash
eth check                           # Check ETH API block consistency
```

## Node Names

Configured in `resources/config.json`:
- **Lotus0** — Primary Lotus node (`http://lotus0:1234/rpc/v1`)
- **Lotus1** — Secondary Lotus node (`http://lotus1:1234/rpc/v1`)
- **Forest0** — Forest node (`http://forest0:3456/rpc/v1`)

## Directory Structure

```
workload/
├── main.go              # CLI entry point
├── main/                # Test Composer scripts
│   ├── anytime_*.sh     # Scripts that run anytime
│   ├── eventually_*.sh  # Eventual consistency checks
│   ├── parallel_*.sh    # Parallel execution scripts
│   └── first_check.sh   # Initial setup
├── resources/           # Go helper functions
│   ├── config.json      # Node configuration
│   ├── connect.go       # RPC connections
│   ├── wallets.go       # Wallet operations
│   ├── mempool_stress.go# Mempool spam
│   ├── eth_methods.go   # ETH API checks
│   ├── consensus.go     # Consensus checks
│   ├── compute.go       # State comparison
│   └── ...
├── entrypoint/          # Container startup
│   ├── entrypoint.sh    # Main entrypoint
│   └── setup-synapse.sh # Synapse SDK setup
└── patches/             # SDK patches
```

## Test Composer Scripts

Located in `main/`:

| Script | Purpose |
|--------|---------|
| `first_check.sh` | Initial setup |
| `anytime_chain_backfill.sh` | Chain index validation |
| `anytime_state_checks.sh` | State consistency |
| `eventually_health_check.sh` | Health monitoring |
| `eventually_comprehensive_health.sh` | Full health check |
| `parallel_driver_create_wallets.sh` | Wallet creation |
| `parallel_driver_spammer.sh` | Transaction spam |
| `parallel_driver_synapse_e2e.sh` | Synapse E2E test |

## Building

```bash
cd workload
docker build -t workload:latest .
```

Or from root:
```bash
make build-workload
```

## Integration

### FilWizard
Contract deployment uses [FilWizard](https://github.com/parthshah1/FilWizard) at `/usr/local/bin/filwizard`.

### Synapse SDK
Storage service testing uses Synapse SDK at `/opt/antithesis/synapse-sdk`.

## Writing New Tests

1. Add CLI command in `main.go`
2. Add helper functions in `resources/`
3. Create Test Composer script in `main/` with appropriate naming:
   - `anytime_` — Runs anytime
   - `parallel_` — Parallel execution
   - `eventually_` — Eventual consistency
   - `serial_` — Sequential execution

### Assertions

Use Antithesis SDK assertions:
```go
assert.Always(condition, "message", details)
assert.Sometimes(condition, "message", details)
assert.Reachable("message", details)
assert.Unreachable("message", details)
```
