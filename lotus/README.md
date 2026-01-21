# Lotus Package

This package builds and configures Lotus Filecoin nodes and miners for the testing environment.

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

## Nodes

### Lotus0 (Primary)
- RPC: `http://lotus0:1234/rpc/v1`
- JWT: `/root/devgen/lotus0/lotus0-jwt`
- Config: `config-0.toml`
- Start script: `lotus-0.sh`

### Lotus1 (Secondary)
- RPC: `http://lotus1:1234/rpc/v1`
- JWT: `/root/devgen/lotus1/lotus1-jwt`
- Config: `config-1.toml`
- Start script: `lotus-1.sh`

## Miners

### lotus-miner0
- Miner ID: `t01000`
- Start script: `lotus-miner-0.sh`
- Depends on: lotus0

### lotus-miner1
- Miner ID: `t01001`
- Start script: `lotus-miner-1.sh`
- Depends on: lotus1

## Patches (`lotus.patch`)

Applied modifications for testing:
- Local Drand configuration instead of public beacons
- F3 consensus parameters (BootstrapEpoch: 20, Finality: 10)
- Dynamic Drand chain info from environment variables

## Configuration

### Config Files
- `config-0.toml` — Lotus0 configuration
- `config-1.toml` — Lotus1 configuration

### Key Settings
- Network bootstrapping
- LibP2P configuration
- Peer discovery
- API permissions

## Artifacts Exported

Each Lotus node exports to shared volume:
- `lotus{N}-jwt` — API authentication token
- `lotus{N}-ipv4addr` — Container IP address
- `lotus{N}-p2pid` — P2P peer ID

## Docker Compose

Defined in `config/docker-compose.yml`:
- lotus0: Port 1234 (RPC)
- lotus1: Port 1234 (RPC)
- lotus-miner0: Mining operations
- lotus-miner1: Mining operations

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
