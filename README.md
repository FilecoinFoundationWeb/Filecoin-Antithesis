# Antithesis Testing with the Filecoin Network

## Purpose

This repository provides a comprehensive testing framework for the Filecoin network using the [Antithesis](https://antithesis.com/) autonomous testing platform. It validates multiple Filecoin implementations (Lotus, Forest, Curio) through deterministic, fault-injected testing.

## Setup Overview

The system runs 12 containers:
- **Drand cluster**: `drand0`, `drand1`, `drand2` (randomness beacon)
- **Lotus nodes**: `lotus0`, `lotus1` (Go implementation)
- **Lotus miners**: `lotus-miner0`, `lotus-miner1`
- **Forest node**: `forest0` (Rust implementation)
- **Curio**: Storage provider with PDP support
- **Yugabyte**: Database for Curio state
- **Workload**: Test orchestration container

## Quick Start

### Prerequisites
- Docker and Docker Compose
- Make

### Build and Run
```bash
# Build all images
make build-all

# Start localnet
make up

# View logs
make logs

# Run tests
docker exec workload /opt/antithesis/workload chain common-tipset
docker exec workload /opt/antithesis/workload mempool spam

# Stop and cleanup
make cleanup
```

### Available Make Commands
```bash
make help           # Show all commands
make build-all      # Build all images
make build-lotus    # Build Lotus image
make build-forest   # Build Forest image  
make build-drand    # Build Drand image
make build-workload # Build workload image
make build-curio    # Build Curio image
make up             # Start containers (docker compose up -d)
make down           # Stop containers
make logs           # Follow logs
make restart        # Restart containers
make cleanup        # Stop and clean data
make show-versions  # Show image version tags
```

## Workload CLI Commands

All commands run inside the workload container:
```bash
docker exec workload /opt/antithesis/workload <command>
```

### Available Commands

| Command | Description |
|---------|-------------|
| `wallet create --node Lotus0 --count 5` | Create and fund wallets |
| `network connect --node Lotus0` | Connect node to peers |
| `network disconnect --node Lotus0` | Disconnect from peers |
| `network reorg --node Lotus0` | Simulate reorg |
| `mempool track --node Lotus0 --duration 5m` | Track mempool size |
| `mempool spam` | Spam transactions across nodes |
| `chain backfill` | Check chain index backfill (Lotus only) |
| `chain common-tipset` | Get common finalized tipset |
| `state check --node Lotus0` | Check state on single node |
| `state compare --epochs 10` | Compare state across all nodes |
| `state compare-at-height --height 100` | Compare state at specific height |
| `consensus check` | Check tipset consensus |
| `monitor comprehensive` | Full health check (peers, F3, height) |
| `monitor height-progression --duration 1m` | Monitor height changes |
| `eth check` | Check ETH API block consistency |

### Node Names (config.json)
- `Lotus0` — First Lotus node
- `Lotus1` — Second Lotus node
- `Forest0` — Forest node

## Test Composer Scripts

Located in `workload/main/`:

| Script | Purpose |
|--------|---------|
| `first_check.sh` | Initial setup validation |
| `anytime_chain_backfill.sh` | Chain index backfill check |
| `anytime_state_checks.sh` | State consistency checks |
| `eventually_health_check.sh` | Health monitoring suite |
| `eventually_comprehensive_health.sh` | Full health check |
| `parallel_driver_create_wallets.sh` | Wallet creation |
| `parallel_driver_spammer.sh` | Transaction spamming |
| `parallel_driver_synapse_e2e.sh` | Synapse SDK e2e test |

## Antithesis Integration

### Fault Injection
Antithesis automatically injects faults (crashes, network partitions, thread pausing) after the workload signals "setup complete".

### SDK Assertions
Test properties use the Antithesis Go SDK:
- `assert.Always()` — Must always hold
- `assert.Sometimes()` — Must hold at least once
- `assert.Reachable()` — Code path must be reached
- `assert.Unreachable()` — Code path must never be reached

### Running Tests on Antithesis
1. Push images to Antithesis registry
2. Use GitHub Actions to trigger tests
3. Review reports in Antithesis dashboard

## Directory Structure

```
├── config/              # Docker compose and env files
├── drand/               # Drand beacon build
├── lotus/               # Lotus node build and scripts
├── forest/              # Forest node build and scripts  
├── curio/               # Curio storage provider build
├── workload/            # Test workload
│   ├── main/            # Test Composer scripts
│   ├── resources/       # Go helper functions
│   ├── entrypoint/      # Container startup scripts
│   └── patches/         # SDK patches
├── shared/              # Shared configs between containers
├── data/                # Runtime data (mount point)
├── Makefile             # Build commands
├── docker-compose.yml   # Service definitions
└── cleanup.sh           # Data cleanup script
```

## Configuration

### Environment Variables
Located in `config/.env`:
- Node data directories
- Port configurations
- Shared volume paths

### Node Configuration
Located in `workload/resources/config.json`:
```json
{
  "nodes": [
    {"name": "Lotus0", "rpcurl": "http://lotus0:1234/rpc/v1", "authtokenpath": "/root/devgen/lotus0/lotus0-jwt"},
    {"name": "Lotus1", "rpcurl": "http://lotus1:1234/rpc/v1", "authtokenpath": "/root/devgen/lotus1/lotus1-jwt"},
    {"name": "Forest0", "rpcurl": "http://forest0:3456/rpc/v1", "authtokenpath": "/root/devgen/forest0/forest0-jwt"}
  ]
}
```

## Contributing

1. Add CLI commands in `workload/main.go`
2. Add helper functions in `workload/resources/`
3. Add Test Composer scripts in `workload/main/` following naming conventions:
   - `anytime_*` — Can run anytime
   - `parallel_*` — Runs in parallel
   - `eventually_*` — Eventual consistency checks
   - `serial_*` — Sequential operations
   - `first_*` — Initial setup

## Documentation

- [Antithesis Documentation](https://antithesis.com/docs/)
- [Lotus Documentation](https://lotus.filecoin.io/)
- [Forest Documentation](https://chainsafe.github.io/forest/)
- [FilWizard](https://github.com/parthshah1/FilWizard) — Contract deployment tool
