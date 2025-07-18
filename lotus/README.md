# Lotus Package

This package contains the configuration and build files for the Lotus Filecoin implementation used in the testing environment.

## Files

### Dockerfile
- **Purpose**: Builds an instrumented version of Lotus with data race detection and coverage instrumentation

### Configuration Files

#### config-1.toml
- **Purpose**: Configuration template for the first Lotus node
- **Usage**: Used in `lotus-1.sh` startup script
- **Features**: Contains network, libp2p, and node-specific settings

#### config-2.toml
- **Purpose**: Configuration template for the second Lotus node
- **Usage**: Used in `lotus-2.sh` startup script
- **Features**: Contains network, libp2p, and node-specific settings

### Patches

#### lotus.patch
- **Purpose**: Custom modifications to Lotus source code for testing environment
- **Key Changes**:
  - Modifies Drand configuration to use local beacon servers instead of public ones
  - Updates F3 consensus parameters (BootstrapEpoch: 20, Finality: 10)
  - Fixes connection handling in hello protocol
  - Adds dynamic Drand chain info loading from environment variables


### Start Scripts Directory (`start_scripts/`)

#### lotus-1.sh
- **Purpose**: Primary Lotus node startup script
- **Features**:
  - Initializes the first Lotus full node
  - Sets up data directory and configuration
  - Handles genesis import and network bootstrapping
  - Configures libp2p networking and peer discovery

#### lotus-2.sh
- **Purpose**: Secondary Lotus node startup script
- **Features**:
  - Starts additional Lotus full node for redundancy
  - Connects to the primary node for network participation
  - Uses different ports and data directories

#### lotus-miner-1.sh
- **Purpose**: First Lotus miner startup script
- **Features**:
  - Initializes mining operations for miner t01000
  - Sets up sector management and storage
  - Configures mining parameters and worker processes

#### lotus-miner-2.sh
- **Purpose**: Second Lotus miner startup script
- **Features**:
  - Initializes mining operations for miner t01001
  - Sets up sector management and storage
  - Configures mining parameters and worker processes

### Testing and Benchmarking

#### lotus_bench.sh
- **Purpose**: Comprehensive benchmarking and testing script for Lotus nodes
- **Features**:
  - Tests various Filecoin RPC methods (ChainHead, WalletBalance, etc.)
  - Tests Ethereum compatibility methods (eth_feeHistory, eth_getBlockByNumber)
  - Performs random method selection and endpoint testing
  - Generates random parameters for testing (epochs, block numbers, addresses)
  - Validates responses and reports performance metrics

## Usage

The Lotus package provides a complete Filecoin node implementation with:
- Full node functionality for blockchain validation
- Mining capabilities for block production
- RPC API for client interactions
- Ethereum compatibility layer
- Data race detection for debugging
- Comprehensive testing and benchmarking tools

Each startup script configures and starts the appropriate Lotus component with the necessary parameters for the testing environment.
