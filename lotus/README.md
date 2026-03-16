# Lotus Package

This package builds and configures Lotus Filecoin nodes and miners for the Antithesis testing environment.

## Overview

Lotus is the reference Go implementation of Filecoin. This package provides:
- Full node functionality for blockchain validation
- Mining capabilities for block production
- RPC API for client interactions
- Ethereum compatibility layer

## Building

```bash
make build-lotus
```

Or directly:
```bash
docker build -t lotus:latest -f lotus/Dockerfile lotus
```

**Note:** The `lotus-adversary` image is built externally from a private fork and assumed to exist as `lotus-adversary:latest` in the Antithesis GCR tenant. It is not built by this repository.

## Nodes

### Lotus0 (Primary)
- RPC: `http://lotus0:1234/rpc/v1`
- JWT: `${LOTUS_0_DATA_DIR}/lotus0-jwt`
- Start script: `scripts/start-lotus.sh 0`

### Lotus1 (Secondary)
- RPC: `http://lotus1:1234/rpc/v1`
- JWT: `${LOTUS_1_DATA_DIR}/lotus1-jwt`
- Start script: `scripts/start-lotus.sh 1`

### Lotus Adversary (Adversarial Full Nodes)

Adversarial nodes use the `lotus-adversary` image and are enumerated (`lotus-adversary0`, `lotus-adversary1`, ...). They join the network as non-mining full nodes.

#### lotus-adversary0
- RPC: `http://lotus-adversary0:1234/rpc/v1`
- JWT: `/root/devgen/lotus-adversary0/lotus-adversary0-jwt`
- Image: `lotus-adversary` (separate image, may diverge from standard lotus builds)
- Start script: `start-lotus-adversary.sh 0`
- No associated miner — joins as a pure full node
- Connects to all lotus, adversary, and forest peers

## Miners

### lotus-miner0
- Miner ID: `t01000`
- Start script: `scripts/start-lotus-miner.sh 0`
- Depends on: lotus0

### lotus-miner1
- Miner ID: `t01001`
- Start script: `scripts/start-lotus-miner.sh 1`
- Depends on: lotus1

## Patches (`lotus.patch`)

Applied modifications for testing:
- Local Drand configuration instead of public beacons
- F3 consensus parameters (BootstrapEpoch: 5, Finality: 2)
- Dynamic Drand chain info from environment variables
- Disabled peer scoring / resource manager for sustained fuzzing

## Configuration

### Config Template
- `lotus-config.toml.template` — Shared template, substituted per-node at startup

### Key Settings
- Network bootstrapping
- LibP2P configuration
- Peer discovery
- API permissions

## Artifacts Exported

Each Lotus node exports to its data directory:
- `lotus{N}-jwt` — API authentication token
- `lotus{N}-ipv4addr` — Container IP address
- `lotus{N}-p2pID` — P2P peer ID

Adversary nodes follow the same pattern with a `lotus-adversary` prefix:
- `lotus-adversary{N}-jwt`
- `lotus-adversary{N}-ipv4addr`
- `lotus-adversary{N}-p2pID`

## Docker Compose

Defined in `docker-compose.yaml` (repo root):
- lotus0: Port 1234 (RPC)
- lotus1: Port 1234 (RPC)
- lotus-adversary0: Port 1234 (RPC) — adversarial full node, no miner
- lotus-miner0: Mining operations
- lotus-miner1: Mining operations

## Dependencies

- **drand0**: Randomness beacon (must be running before Lotus starts)
- **Forest nodes**: Connected as peers after startup

## API Features

### Lotus-Specific (not in Forest)
- `ChainValidateIndex` — Chain backfill check
- Full wallet signing capabilities
- Miner operations

### CommonAPI Compatible
- Chain operations
- State queries
- ETH methods
- Wallet balance/list
