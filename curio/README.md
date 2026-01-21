# Curio Package

This package builds and configures Curio, a Filecoin storage provider with Proof of Data Possession (PDP) capabilities.

## Overview

Curio provides:
- Storage provider functionality for Filecoin
- PDP (Proof of Data Possession) service
- Integration with onchain cloud contracts
- Web GUI for management

## Building

```bash
make build-curio
```

Or directly:
```bash
docker build -t curio:latest -f curio/Dockerfile curio
```

## Configuration

### Environment Variables (set by workload)

Curio reads contract addresses from `/root/devgen/curio/.env.curio`:
```bash
CURIO_DEVNET_PDP_VERIFIER_ADDRESS=0x...
CURIO_DEVNET_FWSS_ADDRESS=0x...
CURIO_DEVNET_SERVICE_REGISTRY_ADDRESS=0x...
CURIO_DEVNET_PAYMENTS_ADDRESS=0x...
CURIO_DEVNET_USDFC_ADDRESS=0x...
CURIO_DEVNET_MULTICALL_ADDRESS=0x...
```

### Ports
- **80**: HTTP endpoint
- **443**: HTTPS endpoint
- **4701**: Web GUI
- **12300**: CLI API

## Start Script (`start_scripts/curio-init.sh`)

Initialization flow:
1. Wait for Lotus node (minimum 10 blocks)
2. Initialize Curio cluster with miner actor
3. Create base configuration with subsystems:
   - CommP, ParkPiece, PDP, MoveStorage, DealMarket, WebGui
4. Set up PDP service (keys, JWT token)
5. Wait for contract addresses from workload container
6. Export contract addresses as environment variables
7. Start Curio with layers: seal, post, pdp-only, gui

## Patches (`curio.patch`)

Applied modifications for Build2k devnet:
- Environment variable support for contract addresses
- Dynamic contract loading from runtime deployments

## Dependencies

- **Lotus**: Uses Lotus binaries for node operations
- **Yugabyte**: Database for state storage
- **Workload**: Provides contract addresses via shared volume

## Contract Integration

Curio integrates with runtime-deployed contracts:
- **PDP Verifier**: Storage proof verification
- **Warm Storage Service (FWSS)**: Storage deal tracking
- **Service Provider Registry**: SP registration
- **USDFC**: Payment token
- **Multicall3**: Batch operations

## Docker Compose

Defined in `config/docker-compose.yml`:
- Depends on: lotus0, yugabyte
- Waits for contract deployment from workload

## Shared Volumes

- `/root/devgen/curio/` — Curio data and private key
- `/root/devgen/curio/.env.curio` — Contract addresses (from workload)
