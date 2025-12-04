# Curio

This package contains the configuration and build files for the Curio storage provider, which provides Filecoin storage services with Proof of Data Possession (PDP) capabilities in the testing environment.

## Overview

Curio is a modern Filecoin storage provider implementation that integrates with Filecoin onchain cloud contracts for storage service operations. It provides:
- Storage provider functionality for Filecoin network
- Proof of Data Possession (PDP) service for storage verification
- Integration with deployed smart contracts (PDP Verifier, Warm Storage Service, Service Provider Registry)
- Web GUI for monitoring and management

## Files

### Dockerfile
- **Purpose**: Builds Curio storage provider with PDP support
- **Key Features**:
  - Uses pre-built Lotus binaries from the `lotus:latest` image
  - Builds Curio from source (branch: `pdpv0`)
  - Includes Rust toolchain (version 1.73.0) for building Curio
  - Builds Curio tools: `curio`, `pdptool`, and `sptool`
  - Fetches Filecoin parameters for 2KiB sectors
  - Supports both amd64 and arm64 architectures

### curio.patch
- **Purpose**: Custom modifications to Curio source code for Build2k devnet support
- **Key Changes**:
  - Adds environment variable support for contract addresses in Build2k mode
  - Makes contract addresses configurable via environment variables:
    - `CURIO_PDP_VERIFIER_ADDRESS`: PDP Verifier contract address
    - `RECORDKEEPER_CONTRACT` or `WARM_STORAGE_CONTRACT_ADDRESS`: Warm Storage Service contract address
    - `SERVICE_REGISTRY_ADDRESS`: Service Provider Registry contract address
    - `USDFC_ADDRESS`: USDFC token contract address
  - Enables dynamic contract address loading from runtime deployments
  - Required for integration with runtime-deployed contracts from the workload container

### Start Scripts Directory (`start_scripts/`)

#### curio-init.sh
- **Purpose**: Curio storage provider initialization and startup script
- **Features**:
  - Waits for Lotus node to be ready and reach minimum block height (10 blocks)
  - Initializes Curio cluster with new miner actor (if not already initialized)
  - Creates base configuration with required subsystems:
    - CommP (Commitment Proof)
    - ParkPiece
    - PDP (Proof of Data Possession)
    - MoveStorage
    - DealMarket
    - WebGui
  - Sets up storage attachment for sealing and storing sectors
  - Configures PDP service:
    - Creates PDP service secret and public key
    - Imports private key for signing PDP proofs
    - Creates PDP service via RPC
    - Generates JWT token for PDP service authentication
  - Waits for contract addresses from workload container (`.env.devnet` file)
  - Exports contract addresses as environment variables:
    - `CURIO_PDP_VERIFIER_ADDRESS`: PDP Verifier contract
    - `RECORDKEEPER_CONTRACT`: Warm Storage Service contract
    - `SERVICE_REGISTRY_ADDRESS`: Service Provider Registry contract
    - `USDFC_ADDRESS`: USDFC token contract
  - Starts Curio node with required layers: seal, post, pdp-only, gui

## Integration with Runtime Contract Deployment

Curio integrates with the runtime contract deployment system:

1. **Contract Address Discovery**: Curio waits for the workload container to deploy contracts and create the `.env.devnet` file containing contract addresses.

2. **Environment Variable Configuration**: The startup script sources contract addresses from `.env.devnet` and exports them as environment variables that Curio's patched code reads.

3. **Required Contracts**:
   - **PDP Verifier**: Verifies Proof of Data Possession proofs submitted by the storage provider
   - **Warm Storage Service (RecordKeeper)**: Tracks storage deals and service records
   - **Service Provider Registry**: Manages storage provider registration and metadata
   - **USDFC**: Token contract for payments and settlements

4. **Initialization Flow**:
   - Curio waits for Lotus node to be ready
   - Creates miner actor and initializes cluster
   - Sets up PDP service with cryptographic keys
   - Waits for contract addresses from workload container
   - Configures contract addresses via environment variables
   - Starts Curio node with PDP and GUI layers enabled

## Usage

The Curio package provides a complete storage provider implementation with:

- **Storage Operations**: Seal, store, and manage Filecoin sectors
- **PDP Service**: Generate and submit Proof of Data Possession proofs
- **Contract Integration**: Interact with deployed Filecoin onchain cloud contracts
- **Web GUI**: Monitor and manage storage provider operations (port 4701)
- **API Access**: RPC API for programmatic access (port 12300)

## Dependencies

- **Lotus**: Curio requires Lotus binaries for Filecoin node operations
- **Yugabyte Database**: Used for storing Curio state and metadata
- **Workload Container**: Provides contract addresses via shared volume

## Configuration

Curio configuration is managed through:
- Environment variables for contract addresses (set by `curio-init.sh`)
- Curio config files created during initialization
- Shared volume (`/root/devgen/curio`) for data persistence and contract address sharing

## Network Ports

- **80**: HTTP service endpoint
- **443**: HTTPS service endpoint (with TLS delegation)
- **4701**: Web GUI interface
- **12300**: Curio CLI API endpoint

## Notes

- Curio runs in Build2k mode for devnet testing
- All contract addresses must be provided via environment variables (no hardcoded addresses)
- The PDP service requires proper key management and JWT token generation
- Storage provider registration with the Service Provider Registry is handled separately via Synapse SDK

