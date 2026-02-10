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

## Stress Engine

The workload container runs a **stress engine** that continuously picks weighted actions ("vectors") and executes them against Lotus and Forest nodes. Each vector uses Antithesis SDK assertions to verify safety and liveness.

### Stress Vectors

| Vector | Env Var | Category | Description |
|--------|---------|----------|-------------|
| `DoTransferMarket` | `STRESS_WEIGHT_TRANSFER` | Mempool | Random FIL transfers between wallets |
| `DoGasWar` | `STRESS_WEIGHT_GAS_WAR` | Mempool | Gas premium replacement racing |
| `DoAdversarial` | `STRESS_WEIGHT_ADVERSARIAL` | Safety | Double-spend, invalid sigs, nonce races |
| `DoHeavyCompute` | `STRESS_WEIGHT_HEAVY_COMPUTE` | Consensus | StateCompute re-execution verification |
| `DoChainMonitor` | `STRESS_WEIGHT_CHAIN_MONITOR` | Consensus | 6 sub-checks: tipset consensus, height progression, peer count, head comparison, state roots, state audit |
| `DoDeployContracts` | `STRESS_WEIGHT_DEPLOY` | FVM/EVM | Deploy EVM contracts via EAM |
| `DoContractCall` | `STRESS_WEIGHT_CONTRACT_CALL` | FVM/EVM | Invoke contracts (recursion, delegatecall, tokens) |
| `DoSelfDestructCycle` | `STRESS_WEIGHT_SELFDESTRUCT` | FVM/EVM | Deploy → destroy → cross-node verify |
| `DoConflictingContractCalls` | `STRESS_WEIGHT_CONTRACT_RACE` | FVM/EVM | Same-nonce contract calls to different nodes |

Weights are configured in `docker-compose.yaml` environment. Set to `0` to disable.

### Reorg Safety

All state-sensitive assertions use `ChainGetFinalizedTipSet` so they are safe during partition → reorg chaos injected by Antithesis.

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
├── workload/            # Stress engine
│   ├── cmd/stress-engine/  # Engine source
│   │   ├── main.go            # Entry point, deck builder, action loop
│   │   ├── helpers.go         # Shared message helpers
│   │   ├── mempool_vectors.go # Transfer, gas war, adversarial
│   │   ├── evm_vectors.go     # Contract deploy, invoke, selfdestruct
│   │   ├── consensus_vectors.go # Heavy compute, chain monitor
│   │   └── contracts.go       # EVM bytecodes, ABI encoding
│   ├── entrypoint/         # Container startup scripts
│   └── Dockerfile
├── shared/              # Shared configs between containers
├── data/                # Runtime data (mount point)
├── Makefile             # Build commands
├── docker-compose.yaml  # Service definitions
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

1. Add a new vector function in the appropriate `*_vectors.go` file
2. Register it in `main.go:buildDeck()` with a `STRESS_WEIGHT_*` env var
3. Add the env var to `docker-compose.yaml`
4. Use Antithesis assertions (`assert.Always`, `assert.Sometimes`) for properties

## Documentation

- [Antithesis Documentation](https://antithesis.com/docs/)
- [Lotus Documentation](https://lotus.filecoin.io/)
- [Forest Documentation](https://chainsafe.github.io/forest/)
- [FilWizard](https://github.com/parthshah1/FilWizard) — Contract deployment tool
