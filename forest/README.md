# Forest Package

This package builds and configures the Forest Filecoin node, a Rust-based implementation used for cross-implementation testing in the Antithesis environment.

## Overview

Forest provides an alternative Filecoin implementation that:
- Validates consensus across different codebases (Rust vs Go/Lotus)
- Tests interoperability between implementations
- Provides different performance characteristics

## Building

```bash
make build-forest
```

Or directly:
```bash
docker build -t forest:latest -f forest/Dockerfile forest
```

## Configuration

### Forest Config Template (`forest_config.toml.tpl`)
- Keystore encryption: disabled (testing)
- Data directory: set via `$FOREST_DATA_DIR`
- Kademlia: disabled (controlled peer environment)
- Target peers: computed from `$NUM_LOTUS_CLIENTS + $NUM_FOREST_CLIENTS - 1`
- Chain type: devnet

### Node Configuration
Node details are defined in `workload/resources/config.json`:
```json
{
  "name": "Forest0",
  "rpcurl": "http://forest0:3456/rpc/v1",
  "authtokenpath": "/forest0/forest0-jwt"
}
```

## Start Script (`scripts/start-forest.sh`)

The startup script:
1. Fetches Drand chain info and formats it for Forest
2. Generates config from template
3. Initializes Forest with genesis
4. Starts Forest daemon
5. Exports artifacts to data directory
6. Imports genesis miner keys for F3 signing
7. Connects to Lotus and other Forest peers

## Artifacts Exported

Each Forest node exports to its data directory:
- `forest{N}-jwt` — API authentication token
- `forest{N}-ipv4addr` — Container IP address
- `forest{N}-p2pid` — P2P peer ID

## API Support

### Supported (CommonAPI compatible)
- `ChainHead`, `ChainGetTipSet`, `ChainGetFinalizedTipSet`
- `StateGetActor`, `StateMinerInfo`, `StateMinerPower`
- `EthGetBlockByNumber`, `EthGetBlockByHash`
- Wallet operations (create, list, balance)

### Not Supported
- `ChainValidateIndex` (chain backfill check) — use `FilterLotusNodes()` for backfill checks
- Some miner-specific operations

## Docker Compose

Defined in `docker-compose.yaml` (repo root):
- Port 3456: RPC API
- Depends on: lotus0, lotus1

## Dependencies

- **drand0**: Randomness beacon (must be running before Forest starts)
- **Lotus nodes**: Genesis file and peer connectivity
