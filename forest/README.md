# Forest Package

This package contains the configuration and build files for the Forest Filecoin implementation, an alternative Rust-based Filecoin node used in the testing environment.

## Files

### Dockerfile
- **Purpose**: Builds an instrumented version of Forest with coverage instrumentation and Antithesis integration


#### forest_config.toml.tpl
- **Purpose**: Configuration template for Forest nodes
- **Usage**: Used in `forest-init.sh` startup script
- **Features**:
  - Disables keystore encryption for testing
  - Sets data directory to `/forest_data`
  - Configures network with Kademlia disabled
  - Sets target peer count to 2
  - Configures chain type as "devnet"

### Start Scripts Directory (`start_scripts/`)

#### forest-init.sh
- **Purpose**: Forest node initialization and startup script
- **Features**:
  - Creates and initializes a new Forest node
  - Sets up data directories and configuration
  - Handles genesis import and network bootstrapping
  - Configures P2P networking and peer discovery
  - Sets up chain synchronization parameters

## Usage

The Forest package provides an alternative Filecoin node implementation with:
- Rust-based blockchain validation and processing
- F3 consensus sidecar integration
- P2P networking and peer management
- Chain synchronization and state management
- Comprehensive coverage instrumentation for testing
- Integration with Antithesis testing framework

The Forest node serves as an alternative implementation to Lotus, providing:
- Different consensus mechanism implementations
- Alternative state management approaches
- Rust-native performance optimizations
- Cross-implementation testing capabilities

Each startup script configures and starts the Forest node with the necessary parameters for the testing environment, enabling comparison and validation between different Filecoin implementations.
