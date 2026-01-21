# Forest Package

This package builds and configures the Forest Filecoin node, a Rust-based implementation used for cross-implementation testing.

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

### Node Configuration
Node details are in `workload/resources/config.json`:
```json
{
  "name": "Forest0",
  "rpcurl": "http://forest0:3456/rpc/v1",
  "authtokenpath": "/root/devgen/forest0/forest0-jwt"
}
```

### Forest Config Template (`forest_config.toml.tpl`)
- Keystore encryption: disabled (testing)
- Data directory: `/forest_data`
- Kademlia: disabled
- Target peers: 2
- Chain type: devnet

## Start Script (`scripts/start-forest.sh`)

The startup script:
1. Generates JWT token for API authentication
2. Imports genesis from Lotus
3. Connects to Lotus peers
4. Exports artifacts to shared volume:
   - `/root/devgen/forest0/forest0-jwt`
   - `/root/devgen/forest0/forest0-ipv4addr`
   - `/root/devgen/forest0/forest0-p2pid`

## API Support

### Supported (CommonAPI compatible)
- `ChainHead`, `ChainGetTipSet`, `ChainGetFinalizedTipSet`
- `StateGetActor`, `StateMinerInfo`, `StateMinerPower`
- `EthGetBlockByNumber`, `EthGetBlockByHash`
- Wallet operations (create, list, balance)

### Not Supported
- `ChainValidateIndex` (chain backfill check)
- Some miner-specific operations

## Docker Compose

Forest is defined in `config/docker-compose.yml`:
- Port 3456: RPC API
- Depends on: lotus0, lotus1
- Volume: `./data/forest0:/forest_data`

## Artifacts Exported

After startup, Forest exports these files to the shared volume:
- `forest0-jwt` — API authentication token
- `forest0-ipv4addr` — Container IP address
- `forest0-p2pid` — P2P peer ID

## Limitations for Testing

1. **Wallet Funding**: Forest wallets must be funded from Lotus (Forest doesn't mine)
2. **Chain Backfill**: Use `FilterLotusNodes()` not `FilterV1Nodes()` for backfill checks
3. **Some RPC Methods**: Not all Lotus methods are implemented
