# Curio Package

This package builds and configures Curio, a Filecoin storage provider with Proof of Data Possession (PDP) capabilities, for the Antithesis testing environment.

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
# Run from repository root:
docker build -t curio:latest -f curio/Dockerfile curio
```

## Configuration

### Environment Variables (set by workload)

Curio reads contract addresses from `$CURIO_REPO_PATH/.env.curio`:
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
1. Wait for Lotus node (minimum 5 epochs)
2. Create wallets and fund them
3. Initialize Curio cluster with miner actor
4. Create base and PDP configuration layers
5. Wait for contract addresses from workload container
6. Attach storage via temporary Curio node
7. Set up PDP service (keys, JWT token)
8. Start Curio with layers: seal, post, pdp-only, gui

## Patches

Applied modifications for testing:
- `increase-cpu-avail-antithesis.patch`: Hardcode CPU count to 32 for Antithesis environment
- `reduce-reservations-size.patch`: Use 2KiB proof type for faster test execution

## Dependencies

- **Lotus**: Uses Lotus binaries for wallet and chain operations
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

Defined in `docker-compose.yaml` (repo root):
- Depends on: lotus0, yugabyte
- Waits for contract deployment from workload

## Shared Volumes

- `$CURIO_REPO_PATH/` — Curio data and private key
- `$CURIO_REPO_PATH/.env.curio` — Contract addresses (from workload)
