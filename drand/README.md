# Drand Package

This package contains the configuration and build files for the Drand distributed randomness beacon service used in the Filecoin testing environment.

## Files

### Dockerfile
- **Purpose**: Builds an instrumented version of the Drand beacon service
- **Key Features**:
  - Based on Go 1.23.2 with Antithesis instrumentation
  - Clones Drand v2.1.3 from GitHub
  - Applies code coverage instrumentation using `antithesis-go-instrumentor`
  - Builds the `drand` binary with insecure mode for testing
  - Creates symbol files for debugging and coverage analysis

### Start Scripts Directory (`start_scripts/`)

#### drand-1.sh
- **Purpose**: Primary Drand beacon node startup script
- **Features**: 
  - Initializes the first Drand beacon node
  - Sets up network configuration and chain parameters
  - Handles genesis setup and network bootstrapping

#### drand-2.sh
- **Purpose**: Secondary Drand beacon node startup script
- **Features**:
  - Starts additional Drand beacon node for redundancy
  - Connects to the primary beacon for network participation

#### drand-3.sh
- **Purpose**: Tertiary Drand beacon node startup script
- **Features**:
  - Provides additional redundancy for the Drand network
  - Ensures high availability of randomness beacon services

## Usage

The Drand package provides distributed randomness beacon services that are essential for:
- Filecoin's consensus mechanism
- Cryptographic randomness generation
- Network security and unpredictability

Each startup script (`drand-1.sh`, `drand-2.sh`, `drand-3.sh`) will execute the corresponding Drand node with appropriate configuration for the testing environment.
