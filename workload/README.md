# Filecoin Antithesis Workload

This directory contains the workload implementation for testing Filecoin nodes using the Antithesis platform. The workload is designed to test various aspects of Filecoin nodes including consensus, networking, state management, and smart contract interactions.

**Note**: Most workload operations (wallet management, contract deployment, transaction testing) are handled by [FilWizard](https://github.com/parthshah1/FilWizard), a separate comprehensive Filecoin testing tool that is integrated into this project. FilWizard is cloned and built during the workload container image build process.

## Directory Structure

- `main.go`: Main entry point with CLI commands for different operations
- `main/`: Test Composer commands for generating test cases
- `resources/`: Helper functions and utilities
- `entrypoint/`: Entry point scripts for workload execution
  - `entrypoint.sh`: Main entrypoint that orchestrates contract deployment and SDK setup
  - `setup-synapse.sh`: Configures Synapse SDK with deployed contract addresses
  - `setup_complete.py`: Signals system readiness to Antithesis

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

The workload container automatically deploys Filecoin onchain cloud contracts during system initialization using FilWizard. This happens in the `entrypoint.sh` script before the system signals readiness.

### Deployment Process

1. **Wait for Chain Readiness**: The entrypoint waits for the blockchain to reach a minimum block height (default: 5 blocks).

2. **Deploy Contracts**: Uses FilWizard to deploy contracts from the Filecoin Synapse configuration:
   ```bash
   filwizard contract deploy-local --config /opt/antithesis/FilWizard/config/filecoin-synapse.json \
     --workspace ./workspace --rpc-url "$FILECOIN_RPC" --create-deployer --bindings
   ```
   
   This command is provided by FilWizard and handles:
   - Contract compilation from source
   - Deployment to the Filecoin network
   - Contract address extraction and storage
   - Go binding generation for programmatic access

3. **Extract Contract Addresses**: FilWizard stores contract addresses in `deployments.json`, which is then shared with other containers via shared volumes.

4. **Configure Synapse SDK**: The `setup-synapse.sh` script:
   - Extracts all contract addresses from FilWizard's `deployments.json`
   - Uses FilWizard to create client and Storage Provider (SP) private keys via `filwizard wallet create`
   - Uses FilWizard to fund accounts with USDFC tokens and FIL via `filwizard payments mint-private-key`
   - Creates `.env.devnet` file with all configuration for Synapse SDK
   - Shares configuration with Curio container

### Deployed Contracts

- **USDFC**: ERC-20 token contract for payments and settlements
- **Multicall3**: Batch transaction contract for efficient multi-call operations
- **FilecoinWarmStorageService**: Main warm storage service contract
- **FilecoinWarmStorageServiceStateView**: State view contract for querying storage service state
- **ServiceProviderRegistry**: Registry contract for managing storage providers
- **PDPVerifier**: Proof of Data Possession verifier contract for storage proofs

### Synapse SDK Integration

The Synapse SDK is automatically set up to interact with deployed contracts:
- Located at `/opt/antithesis/synapse-sdk`
- Configured via `.env.devnet` file with all contract addresses
- Provides JavaScript/TypeScript APIs for:
  - Storage provider registration
  - Storage deal creation and management
  - Payment processing
  - PDP proof submission and verification

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

- **Foundry**: A fast, portable, and modular toolkit for Ethereum application development written in Rust. Foundry includes:
  - **forge**: Compile, deploy, and test EVM-compatible smart contracts. Example: `forge build` to compile contracts, `forge test` to run Solidity tests, and `forge create` to deploy contracts.
  - **cast**: Interact with deployed contracts and send transactions. Example: `cast call` to query contract state, `cast send` to invoke contract methods.
  - **anvil**: Local Ethereum node for rapid testing and development. Example: `anvil` to start a local testnet for contract deployment and interaction.

- **Synapse SDK**: JavaScript/TypeScript SDK for interacting with Filecoin storage services and deployed contracts. Provides high-level APIs for storage operations, provider management, and payment processing.

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

