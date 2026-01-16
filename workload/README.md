# Filecoin Antithesis Workload

This directory contains the workload implementation for testing Filecoin nodes using the Antithesis platform. The workload is designed to test various aspects of Filecoin nodes including consensus, networking, state management, and smart contract interactions.

**Note**: Most workload operations (wallet management, contract deployment, transaction testing) are handled by [FilWizard](https://github.com/parthshah1/FilWizard), a separate comprehensive Filecoin testing tool that is integrated into this project. FilWizard is cloned and built during the workload container image build process.

## Directory Structure

- `main.go`: Main entry point with CLI commands for different operations
- `main/`: Test Composer commands for generating test cases
- `resources/`: Helper functions and utilities
- `entrypoint/`: Entry point scripts for workload execution
  - `entrypoint.sh`: Main entrypoint that handles contract deployment and Curio environment setup
  - `setup-synapse.sh`: Configures Synapse SDK with deployed contract addresses and registers service provider
  - `setup_complete.py`: Signals system readiness to Antithesis
- `patches/`: Patch files for modifying dependencies
  - `synapse-sdk.patch`: Patches Synapse SDK to support devnet pricing model and capability encoding

## FilWizard Integration

[FilWizard](https://github.com/parthshah1/FilWizard) is a comprehensive Filecoin testing tool that provides:
- **Wallet Management**: Create and manage Filecoin and Ethereum wallets
- **Transaction Testing**: Send individual transactions or spam the mempool with high-volume transaction loads
- **Smart Contract Operations**: Deploy, call, and manage smart contracts (both Foundry and Hardhat projects)
- **Automated Deployment**: Deploy contracts from Git repositories with full automation support
- **Go Bindings Generation**: Generate Go bindings for deployed contracts using abigen

FilWizard is integrated into the workload container and is used for all contract deployment and wallet operations. The FilWizard repository is cloned during the Docker build process and the `filwizard` binary is installed at `/usr/local/bin/filwizard`.

For detailed FilWizard documentation, see: https://github.com/parthshah1/FilWizard

## Runtime Contract Deployment

The workload container automatically deploys Filecoin onchain cloud contracts during system initialization using FilWizard. The deployment process is split across two scripts for better separation of concerns:

### Deployment Process Overview

The initialization process follows these steps:

1. **`entrypoint.sh`** handles contract deployment and Curio configuration
2. **`setup-synapse.sh`** handles Synapse SDK setup and service provider registration

### Step 1: Contract Deployment (`entrypoint.sh`)

The `entrypoint.sh` script performs the following operations:

1. **Time Synchronization**: Synchronizes system time with NTP servers
2. **Wait for Chain Readiness**: Waits for the blockchain to reach a minimum block height (default: 5 blocks)
3. **Deploy Contracts**: Uses FilWizard to deploy contracts from the Filecoin Synapse configuration:
   ```bash
   filwizard contract deploy-local --config /opt/antithesis/FilWizard/config/filecoin-synapse.json \
     --workspace ./workspace --rpc-url "$FILECOIN_RPC" --create-deployer --bindings
   ```
   
   This command handles:
   - Contract compilation from source
   - Deployment to the Filecoin network
   - Contract address extraction and storage
   - Go binding generation for programmatic access

4. **Extract Contract Addresses**: 
   - Extracts service contract addresses from `workspace/filecoinwarmstorage/service_contracts/deployments.json` (flat object format)
   - Extracts USDFC and Multicall3 from `workspace/deployments.json` (array format)
   - Merges all addresses into a shared `/root/devgen/deployments.json` file

5. **Create Curio Environment File**: Creates `/root/devgen/curio/.env.curio` with Curio-specific environment variables:
   - `CURIO_DEVNET_PDP_VERIFIER_ADDRESS`
   - `CURIO_DEVNET_FWSS_ADDRESS`
   - `CURIO_DEVNET_SERVICE_REGISTRY_ADDRESS`
   - `CURIO_DEVNET_PAYMENTS_ADDRESS`
   - `CURIO_DEVNET_USDFC_ADDRESS`
   - `CURIO_DEVNET_MULTICALL_ADDRESS`

### Step 2: Synapse SDK Setup (`setup-synapse.sh`)

The `setup-synapse.sh` script performs the following operations:

1. **Load Contract Addresses**: Reads all contract addresses from the shared `/root/devgen/deployments.json` file

2. **Create Wallets**:
   - Loads deployer private key from `accounts.json` (created by FilWizard during deployment)
   - Creates client wallet using `filwizard wallet create`
   - Waits for and loads SP private key from Curio container (`/root/devgen/curio/private_key`)

3. **Create Synapse Environment File**: Creates `/opt/antithesis/synapse-sdk/.env.localnet` with Synapse SDK configuration:
   - Network settings (devnet, chain ID, RPC URLs)
   - Contract addresses with `LOCALNET_*` prefix
   - Private keys (deployer, SP, client)
   - Service provider configuration

4. **Mint Tokens**: Uses FilWizard to mint tokens for client and SP:
   - Client: 1000 USDFC + 10 FIL
   - SP: 10000 USDFC + 10 FIL

5. **Register Service Provider**: Uses `post-deploy-setup.js` to:
   - Register the service provider in the SP Registry
   - Configure PDP (Proof of Data Possession) product offering
   - Set up payment and storage configurations

6. **Run E2E Tests**: Executes `example-storage-e2e.js` to verify end-to-end storage operations

### Deployed Contracts

The following contracts are deployed and configured:

- **USDFC**: ERC-20 token contract for payments and settlements
- **Multicall3**: Batch transaction contract for efficient multi-call operations
- **FilecoinWarmStorageService (FWSS)**: Main warm storage service contract (proxy address used)
- **FilecoinWarmStorageServiceStateView (FWSS_VIEW)**: State view contract for querying storage service state
- **ServiceProviderRegistry (SP_REGISTRY)**: Registry contract for managing storage providers (proxy address used)
- **PDPVerifier**: Proof of Data Possession verifier contract for storage proofs (proxy address used)
- **FilecoinPay (PAYMENTS)**: Payment contract for handling storage payments

**Note**: Proxy addresses are used for all service contracts to enable upgradeability. The implementation addresses are also stored but not used for interactions.

### Contract Address Storage

Contract addresses are stored in multiple locations:

1. **`/root/devgen/deployments.json`**: Shared deployments file containing all contract addresses, accessible by both workload and Curio containers
2. **`/root/devgen/curio/.env.curio`**: Curio-specific environment file with `CURIO_DEVNET_*` prefixed variables
3. **`/opt/antithesis/synapse-sdk/.env.localnet`**: Synapse SDK environment file with `LOCALNET_*` prefixed variables

### Synapse SDK Integration

The Synapse SDK is automatically set up to interact with deployed contracts:

- **Location**: `/opt/antithesis/synapse-sdk` (cloned from `FilOzone/synapse-sdk` master branch)
- **Configuration**: `.env.localnet` file with all contract addresses and network settings
- **Patch Applied**: `patches/synapse-sdk.patch` modifies the SDK to:
  - Support devnet pricing model (per-day instead of per-month)
  - Use proper capability encoding for PDP offerings
  - Handle location format correctly (X.509 DN format)
- **Utilities Used**:
  - `post-deploy-setup.js`: Registers service provider and configures PDP product
  - `example-storage-e2e.js`: Runs end-to-end storage tests
- **APIs Provided**:
  - Storage provider registration
  - Storage deal creation and management
  - Payment processing
  - PDP proof submission and verification

## Environment Variables

The workload uses different environment variable naming conventions for different services:

### Curio Environment Variables

Curio uses `CURIO_DEVNET_*` prefixed variables in `/root/devgen/curio/.env.curio`:
- `CURIO_DEVNET_PDP_VERIFIER_ADDRESS`
- `CURIO_DEVNET_FWSS_ADDRESS`
- `CURIO_DEVNET_SERVICE_REGISTRY_ADDRESS`
- `CURIO_DEVNET_PAYMENTS_ADDRESS`
- `CURIO_DEVNET_USDFC_ADDRESS`
- `CURIO_DEVNET_MULTICALL_ADDRESS`

### Synapse SDK Environment Variables

Synapse SDK uses `LOCALNET_*` prefixed variables in `/opt/antithesis/synapse-sdk/.env.localnet`:
- `NETWORK=devnet`
- `LOCALNET_CHAIN_ID=31415926`
- `LOCALNET_RPC_URL=http://lotus0:1234/rpc/v1`
- `LOCALNET_RPC_WS_URL=ws://lotus0:1234/rpc/v1`
- `LOCALNET_MULTICALL3_ADDRESS`
- `LOCALNET_USDFC_ADDRESS`
- `LOCALNET_WARM_STORAGE_CONTRACT_ADDRESS`
- `LOCALNET_WARM_STORAGE_VIEW_ADDRESS`
- `LOCALNET_SP_REGISTRY_ADDRESS`
- `LOCALNET_PDP_VERIFIER_ADDRESS`
- `LOCALNET_PAYMENTS_ADDRESS`
- `DEPLOYER_PRIVATE_KEY`
- `SP_PRIVATE_KEY`
- `CLIENT_PRIVATE_KEY`
- `SP_SERVICE_URL`

## Smart Contract Tooling

This environment provides several powerful tools for developing, compiling, deploying, and testing smart contracts on Filecoin-compatible networks. Below are the main tools available for smart contract workflows:

- **FilWizard** ([GitHub](https://github.com/parthshah1/FilWizard)): Comprehensive Filecoin testing tool that handles most workload operations including:
  - Wallet creation and management (Filecoin and Ethereum wallets)
  - Contract deployment from Git repositories or local configurations
  - Transaction testing and mempool spamming
  - Contract interaction and method calls
  - Go bindings generation for deployed contracts
  - Payment operations and token minting
  
  FilWizard is the primary tool used for all contract deployment and wallet operations in this testing environment. It's integrated as a dependency and installed during the workload container build.
  
  **Accounts File**: FilWizard creates `workspace/accounts.json` with the following structure:
  ```json
  {
    "accounts": {
      "deployer": {
        "privateKey": "...",
        "address": "..."
      },
      "client": {
        "privateKey": "...",
        "address": "..."
      }
    }
  }
  ```

- **Foundry**: A fast, portable, and modular toolkit for Ethereum application development written in Rust. Foundry includes:
  - **forge**: Compile, deploy, and test EVM-compatible smart contracts. Example: `forge build` to compile contracts, `forge test` to run Solidity tests, and `forge create` to deploy contracts.
  - **cast**: Interact with deployed contracts and send transactions. Example: `cast call` to query contract state, `cast send` to invoke contract methods.
  - **anvil**: Local Ethereum node for rapid testing and development. Example: `anvil` to start a local testnet for contract deployment and interaction.

- **Synapse SDK** ([GitHub](https://github.com/FilOzone/synapse-sdk)): JavaScript/TypeScript SDK for interacting with Filecoin storage services and deployed contracts. Provides high-level APIs for storage operations, provider management, and payment processing.
  
  The SDK is patched during the Docker build process to support:
  - Devnet network configuration
  - Per-day pricing model (instead of per-month)
  - Proper PDP capability encoding
  - X.509 DN location format
  
  **Key Utilities**:
  - `utils/post-deploy-setup.js`: Registers service providers and configures PDP products
  - `utils/example-storage-e2e.js`: End-to-end storage test suite
  - `utils/sp-tool.js`: Service provider management tool (mainnet/calibration only)

- **PDP (Proof of Data Possession)**: A set of smart contracts and cryptographic tools (from [FilOzone/pdp](https://github.com/FilOzone/pdp)) for testing data possession proofs and related contract logic. Example: Build and deploy PDP contracts for Filecoin storage proofs.

- **Payments Service**: A suite of smart contracts and utilities (from [FilOzone/filecoin-services-payments](https://github.com/FilOzone/filecoin-services-payments)) for payment flows and settlement on Filecoin. Example: Use `forge build` and `forge create` to deploy payment contracts.

- **Node.js & npm**: Useful for running JavaScript/TypeScript-based smart contract tools, scripts, or frameworks (e.g., Hardhat, Truffle, or custom deployment scripts). Example: Install and use Hardhat for advanced contract deployment scenarios.

- **Rust**: Required for building Foundry and may be used for advanced contract development or integration with Rust-based Filecoin clients (e.g., Forest).

These tools are pre-installed and ready to use in the container. You can:
- Deploy Filecoin onchain cloud contracts using FilWizard
- Compile and deploy EVM-compatible contracts (SimpleCoin, MCopy, TransientStorage, etc.)
- Run Solidity tests and interact with contracts using Foundry
- Use Synapse SDK for end-to-end storage service testing
- Use PDP and payments contracts for Filecoin-specific workflows
- Leverage Node.js for scripting or integrating with other Ethereum tooling

Refer to the documentation of each tool for advanced usage and integration with the Filecoin test environment.

## Writing New Workloads

There are several ways to add new workloads to test Filecoin nodes:

### 1. Adding CLI Commands (main.go)

The CLI uses `urfave/cli` for command structure. To add a new command:

1. Identify the appropriate command group (wallet, network, mempool, contracts, consensus, monitoring)

2. Add your command to the relevant group function:
```go
func someCommandGroup() *cli.Command {
    return &cli.Command{
        Name:  "group-name",
        Usage: "Group description",
        Subcommands: []*cli.Command{
            {
                Name:  "my-new-command",
                Usage: "Description of what the command does",
                Flags: []cli.Flag{
                    // Define command flags here
                },
                Action: func(c *cli.Context) error {
                    return performMyNewOperation(c.Context)
                },
            },
        },
    }
}
```

3. Implement the operation function with appropriate assertions:
```go
func performMyNewOperation(ctx context.Context) error {
    log.Printf("[INFO] Starting operation...")
    
    // Test your conditions using assertions
    assert.Sometimes(condition, "Expected behavior occurred", details)
    
    // Your operation logic here
    result := someOperation()
    
    // Assert the results
    assert.Always(result.isValid(), "Operation result should be valid", 
        map[string]interface{}{
            "result": result,
            "details": "Additional context about the assertion"
        })
    
    return nil
}
```

### 2. Adding Helper Functions (resources/)

The `resources/` directory contains reusable helper functions. To add new helpers:

1. Create a new file in `resources/` if it's a new category of functionality
2. Add your helper functions with proper documentation
3. Export functions that will be used by workloads

Example:
```go
// my_helper.go
package resources

// MyNewHelper performs a specific operation
// It returns an error if the operation fails
func MyNewHelper(ctx context.Context, api api.FullNode) error {
    log.Printf("[INFO] Starting helper operation...")
    // Helper implementation
    return nil
}
```

### 3. Writing Tests with Assertions

Tests in this workload use the Antithesis assertion framework to verify properties and behaviors. Here's how to write effective tests:

1. Define clear test properties:
```go
// Test that an operation completes successfully
assert.Sometimes(operationSucceeded, "Operation should succeed", 
    map[string]interface{}{
        "operation": "description",
        "impact": "What failure means for the system"
    })

// Test that invalid states never occur
assert.Always(isValidState, "System should maintain valid state",
    map[string]interface{}{
        "state": currentState,
        "requirement": "Why this state must be maintained"
    })
```

2. Use appropriate assertion types:
```go
// For properties that must always hold
assert.Always(condition, "Property description", details)

// For properties that must occur at least once
assert.Sometimes(condition, "Property description", details)

// For code paths that must be reached
assert.Reachable("Description of the reached state", details)

// For code paths that should never be reached
assert.Unreachable("Description of the unreachable state", details)
```

3. Provide detailed context in assertion details:
```go
details := map[string]interface{}{
    "component": "Which part of the system",
    "expected": expectedValue,
    "actual": actualValue,
    "impact": "Impact of failure",
    "requirement": "Why this assertion matters"
}
```

### 4. Adding Test Composer Commands (main/)

Test Composer commands in `main/` follow naming conventions:
- `parallel_*`: Commands that can run in parallel
- `anytime_*`: Commands that can run at any time
- `eventually_*`: Commands that verify eventual properties

To add a new command:

1. Create a new script following the naming convention
2. Use appropriate Test Composer annotations
3. Implement the test logic

Example:
```bash
#!/bin/bash
# anytime_my_test.sh

# Use the CLI command you created
/opt/antithesis/app group-name my-new-command --flag value
```

## Using Antithesis Assertions

The workload uses the Antithesis Go SDK for defining test properties. Key assertion types:

- `assert.Always(condition, message, details)`: Property must hold every time
- `assert.Sometimes(condition, message, details)`: Property must hold at least once
- `assert.Reachable(message, details)`: Code must be reached
- `assert.Unreachable(message, details)`: Code must never be reached
- `assert.AlwaysOrUnreachable(condition, message, details)`: If reached, condition must hold

For detailed documentation on assertions, see:
[Antithesis Go Assert Documentation](https://antithesis.com/docs/generated/sdk/golang/assert/)

## Best Practices

1. **Error Handling**: Always return meaningful errors and use proper error wrapping
2. **Logging**: Use structured logging with appropriate log levels (`[INFO]`, `[WARN]`, `[ERROR]`)
3. **Context**: Pass context.Context through the call chain for proper cancellation
4. **Configuration**: Use the config.json for configuration settings
5. **Assertions**: Use appropriate assertion types based on the property being tested
6. **Documentation**: Add comments explaining complex logic and test scenarios
7. **Resource Cleanup**: Ensure proper cleanup of resources in defer statements
8. **CLI Structure**: Follow the established command group organization
9. **Code Reuse**: Create helper functions in resources/ for shared functionality
10. **Test Properties**: Make assertions clear and provide detailed context in details map

## Example Workflow

Here's a typical workflow for adding a new test:

1. Identify the test scenario and which command group it belongs to
2. Add helper functions in `resources/` if needed
3. Add CLI command in `main.go` using the urfave/cli structure
4. Add appropriate assertions to verify test properties
5. Create Test Composer command in `main/`
6. Test and validate the implementation

