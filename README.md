# Antithesis Testing with the Filecoin Network

## Purpose

This README serves as a guide for both prospective and active contributers. We will walk through the setup structure, using Antithesis, best practices for contributions, and how to interpret the reports. You should be able to walk away with the purpose of continuous testing, why it is important, the current state of this project, and hopefully ideas to contribute!

## Setup

There are 11 containers running in this system: 3 make up a drand cluster (`drand-1`, `drand-2`, `drand-3`), 2 lotus nodes (`lotus-1`, `lotus-2`), 1 forest node (`forest`), 2 lotus miners (`lotus-miner-1`, `lotus-miner-2`), 1 Curio storage provider (`curio`), 1 Yugabyte database (`yugabyte`), and 1 `workload` that ["makes the system go"](https://antithesis.com/docs/getting_started/basic_test_hookup/).

The `workload` container has the [test commands](https://antithesis.com/docs/test_templates/first_test/#test-commands) where endpoints are called, smart contracts deployed, transactions requested, etc... There are also validations to assert correctness and guarantees also occur in this container using the [Antithesis SDK](https://antithesis.com/docs/using_antithesis/sdk/). We explain more on the SDK in a later section.

### Runtime Contract Deployment

During system initialization, the `workload` container automatically deploys Filecoin onchain cloud contracts to the network using [FilWizard](https://github.com/parthshah1/FilWizard), a comprehensive Filecoin testing tool that handles wallet management, contract deployment, and transaction testing. FilWizard is integrated as a dependency in the workload container. These contracts are deployed at runtime after the blockchain reaches a minimum block height (default: 5 blocks). The deployment process includes:

**Deployed Contracts:**
- **USDFC**: ERC-20 token contract for payments and settlements
- **Multicall3**: Batch transaction contract for efficient multi-call operations
- **FilecoinWarmStorageService**: Main warm storage service contract
- **FilecoinWarmStorageServiceStateView**: State view contract for querying storage service state
- **ServiceProviderRegistry**: Registry contract for managing storage providers
- **PDPVerifier**: Proof of Data Possession verifier contract for storage proofs

**Deployment Flow:**
1. The workload container waits for the chain to reach the initial block height
2. Contracts are deployed using FilWizard's `filwizard contract deploy-local` command with the Filecoin Synapse configuration
3. FilWizard extracts and stores contract addresses in `deployments.json`
4. Contract addresses are shared with other containers (e.g., Curio) via shared volumes
5. The Synapse SDK is configured with contract addresses for E2E testing
6. FilWizard is used to create and fund client and Storage Provider (SP) accounts with USDFC tokens and FIL

**Synapse SDK Integration:**
The Synapse SDK is automatically set up in the workload container to interact with the deployed contracts. It:
- Uses FilWizard to create client and SP private keys
- Uses FilWizard to fund accounts with USDFC tokens and FIL
- Configures environment variables for contract interactions
- Enables end-to-end testing of the Filecoin storage service ecosystem

**Curio Integration:**
The Curio storage provider container:
- Waits for contract addresses from the workload container
- Configures PDP (Proof of Data Possession) service using the deployed PDP Verifier contract
- Sets up storage provider registration with the Service Provider Registry
- Integrates with the Warm Storage Service for storage operations

## Github Files and Directories

In this repository, we have directories that build all the images referenced in the setup above. 

We've made small patches for the Lotus and Forest nodes to work with a local Drand cluster. Antithesis is fully deterministic and requires our SUT to run without internet access (a source of nondeterminism).

**FilWizard Integration**: The workload container integrates [FilWizard](https://github.com/parthshah1/FilWizard), a separate comprehensive Filecoin testing tool repository. FilWizard is cloned during the Docker build process and provides wallet management, contract deployment, and transaction testing capabilities. Most workload operations are handled by FilWizard rather than code in this repository.

The `cleanup.sh` executable will clear the data directory. This data directory is used when running the docker-compose locally, so emptying this is necessary after shutting down the system.

There are supplementary READMEs located in the drand, forest, lotus, workload, and curio directories. These provide some description specific to their respective folders. 

## Sanity Check Locally

A good practice to confirm your test script works correctly in Antithesis is to run it locally. Here are the steps:

1. Build each image required by the docker-compose.yml. We need a total of 5 images (`lotus:latest`, `forest:latest`, `drand:latest`, `workload:latest`, `curio:latest`). The build system supports both local development builds and instrumented builds for Antithesis testing.

You can build all images using:
```bash
# For local development (no instrumentation)
make all LOCAL_BUILD=1

# For Antithesis instrumented builds
make all LOCAL_BUILD=0
```

You can also build individual services:
```bash
# For local development builds
make build-lotus LOCAL_BUILD=1     # Builds lotus without instrumentation
make build-forest LOCAL_BUILD=1    # Builds forest without instrumentation
make build-drand LOCAL_BUILD=1     # Builds drand without instrumentation
make build-workload               # Builds workload (not affected by LOCAL_BUILD)
# Note: Curio image must be built separately - see curio/Dockerfile

# For Antithesis instrumented builds
make build-lotus LOCAL_BUILD=0     # Builds lotus with instrumentation
make build-forest LOCAL_BUILD=0    # Builds forest with instrumentation
make build-drand LOCAL_BUILD=0     # Builds drand with instrumentation
```

The build system will automatically detect your architecture (amd64/x86_64 or arm64/aarch64) and use the appropriate Go and Rust toolchains.

For a full list of available build targets and options:
```bash
make help
```

2. Run `docker-compose up` from the root directory to start all containers defined in `docker-compose.yml`

   **Note:** The workload container will automatically:
   - Wait for the blockchain to reach the initial block height
   - Deploy Filecoin onchain cloud contracts (USDFC, Warm Storage Service, Service Provider Registry, PDP Verifier, etc.)
   - Set up the Synapse SDK with contract addresses
   - Create and fund client and SP accounts
   - Signal `setupComplete` when ready

3. After the workload container has signaled `setupComplete` (or printed `system is healthy`), you can run any test command 1 to many times via `docker exec`:

`docker exec workload /opt/antithesis/test/v1/main/parallel_driver_create_wallets.sh`

4. We should see the command successfully complete. You've now validated this test is ready to run on the Antithesis platform! (Note that SDK assertions won't be evaluated locally).

5. When finished, run `docker-compose down` to stop all the running containers. Complete a local iteration cycle by running the `cleanup.sh` command.

## Using Antithesis

Antithesis is an autonomous testing platform that finds the bugs in your software, with perfect reproducibility to help you fix them.

### Antithesis Fault Injector

Antithesis generates various failure scenarios. The FileCoin system should be resilient to these faults since they happen in production! We automate the process of injecting faults (e.g., crashes, network partitions, thread pausing) into the system, as well as observing system metrics like unexpected container exits and memory usage.

Note: Faults are not injected into the SUT until a "setup_complete" message is emitted. This message is emitted from the `entrypoint.py` script in the `workload` container.

### Antithesis SDK & Test Properties

To generate test cases, Antithesis relies on **test properties** you define. This short video walks through defining SDK assertions within the `workload` container. Assertions can defined in any container in the SUT.

[<img src="1.png">](https://drive.google.com/file/d/1x5VbelH-0WmMvIV4u8vWOgR046A0oubq/view?usp=drive_link)

### Triaging the Report and viewing your Test Properties

Triaging the reports is critical to determine if any of your test properties failed. This short video walks through the report test properties and how they relate to the assertions defined in the `workload` container.

[<img src="2.png">](https://drive.google.com/file/d/1ESQRLXBJitEv9H5e0mcAe6yWiylu0MPd/view?usp=drive_link)

### Running an Antithesis Test from GitHub

To run a manual Antithesis Test, we have implemented GitHub actions. There is also a cron job for nightly 10 hour runs. This short video explains how to run these actions with the branch your test properties are defined on.

[<img src="3.png">](https://drive.google.com/file/d/1dFBuBnVcFcE-vSFsnIh-jcSVY5m9ALQK/view?usp=drive_link)

### Antithesis Test Composer

[The Antithesis Test Composer](https://antithesis.com/docs/test_templates/first_test/) is a framework that gives the Antithesis system control over what is being executed. Hundreds of thousands of different scenarios are executed during a long enough test. It looks for executables with a specific naming convention in a specific directory (explained in the video below).

[<img src="4.png">](https://drive.google.com/file/d/1MLk_NAVMfq5BsBT_DPkiksqh5oSQpB2m/view?usp=drive_link)

For more details, refer to the [Antithesis Documentation](https://antithesis.com/docs/introduction/how_antithesis_works/).

## How to Contribute

Contributions to the project can include iterating on test templates, improving test properties, or enhancing the setup. Below are guidelines for adding tests:

1.  **Creating CLI Flags:**

    -   Add a new CLI flag in `main.go`.
    -   Use the helper RPC wrapper (`rpc.py`) for Forest, if needed.

2.  **Test Structure:**

    -   Place the new test in the `main` directory.
    -   Follow naming conventions (e.g., `parallel_driver_test.sh`, `anytime_test.sh`).
    -   Refer to [Antithesis Test Composer Reference](https://antithesis.com/docs/test_templates/test_composer_reference/) for templates.

3.  **Examples:**

    -   Initialize wallets using `first_check.sh`.
    -   Run tests such as `anytime_node_height_progression.sh` or `parallel_driver_spammer.py`.

## Testing Overview

Our Filecoin testing framework comprehensively validates the entire Filecoin ecosystem through multiple testing categories and operations. Here's what we're testing:

### Test Categories

| Category | Purpose | Test Operations | Next Steps |
|----------|---------|-----------------|------------|
| **Wallet Management** | Validate wallet creation, funding, and deletion across nodes | • Create wallets with random counts (1-15) on random nodes<br>• Delete random number of wallets<br>• Initialize wallets with funding<br>• Verify wallet operations across Lotus1/Lotus2 | |
| **Smart Contract Deployment** | Test EVM-compatible smart contract deployment and interaction | • Deploy SimpleCoin contract (ERC-20 token)<br>• Deploy MCopy contract (memory operations)<br>• Deploy TStorage contract (transient storage)<br>• Runtime deployment of Filecoin onchain cloud contracts (USDFC, Warm Storage Service, Service Provider Registry, PDP Verifier)<br>• Invoke contract methods and verify results | |
| **Transaction Processing** | Validate mempool operations and transaction handling | • Spam transactions between wallets<br>• Mempool fuzzing with different strategies<br>• ETH legacy transaction testing<br>• Random transaction parameter generation | |
| **Consensus & Finality** | Ensure consensus mechanisms work correctly | • Check F3 consensus running status<br>• Validate finalized tipsets match across nodes<br>• Chain walk validation (10 tipsets)<br>• Consensus fault injection testing | |
| **Network Operations** | Test P2P networking and peer management | • Check peer connections across nodes<br>• Monitor network connectivity<br>• Validate node synchronization status<br>• Test network partition resilience | |
| **State Consistency** | Verify blockchain state integrity | • State mismatch detection<br>• Chain index backfill validation<br>• Block height progression monitoring<br>• State consistency across nodes | |
| **Ethereum Compatibility** | Test ETH API compatibility layer | • ETH methods consistency validation<br>• Block retrieval by number vs hash<br>• ETH RPC method testing<br>• Legacy transaction support | |
| **Node Health & Monitoring** | Monitor node health and performance | • Forest node health checks<br>• Node synchronization status<br>• Block height progression timing<br>• System quiescence validation | |
| **Stress Testing** | Validate system under load | • Maximum message size stress tests<br>• Concurrent transaction processing<br>• Memory and resource usage under load<br>• Performance degradation detection | |
| **RPC & API Testing** | Validate API endpoints and responses | • RPC benchmark testing<br>• API response validation<br>• Method parameter testing<br>• Error handling validation | |

### Test Execution Patterns

**Parallel Drivers**: Execute operations concurrently across multiple nodes
- `parallel_driver_*.sh` - Concurrent operations for load testing
- `parallel_driver_*.py` - Python-based parallel operations

**Anytime Tests**: Can run at any time during test execution
- `anytime_*.sh` - Continuous monitoring and validation
- `anytime_*.py` - Python-based anytime operations

**Eventually Tests**: Validate eventual consistency properties
- `eventually_*.py` - Long-running consistency checks

**Serial Drivers**: Sequential operations for setup and cleanup
- `serial_driver_*.sh` - Sequential operations
- `serial_driver_*.py` - Python-based sequential operations

**First Checks**: Initial setup and validation
- `first_check.sh` - System initialization and wallet setup

### Key Testing Features

1. **Deterministic Testing**: All tests run in a fully deterministic environment without internet access
2. **Fault Injection**: Antithesis automatically injects faults (crashes, network partitions, thread pausing)
3. **Cross-Implementation Validation**: Tests both Lotus (Go) and Forest (Rust) implementations
4. **Comprehensive Coverage**: Tests wallet, contract, consensus, networking, and API layers
5. **Performance Monitoring**: Validates timing constraints and performance degradation
6. **State Consistency**: Ensures blockchain state remains consistent across nodes
7. **Ethereum Compatibility**: Validates ETH API compatibility layer functionality
8. **Runtime Contract Deployment**: Automatically deploys Filecoin onchain cloud contracts during system initialization
9. **Storage Provider Testing**: Tests Curio storage provider integration with deployed contracts
10. **End-to-End Storage Workflows**: Validates complete storage service workflows using Synapse SDK

### Assertion Framework

We use the Antithesis SDK to define test properties and assertions:
- **Always Assertions**: Properties that must always hold true
- **Sometimes Assertions**: Properties that should hold true in most cases
- **Unreachable Assertions**: States that should never be reached
- **Timing Assertions**: Performance and timing constraints

## Todo

### Completed Tasks

-   Implement comprehensive wallet management (create, fund, delete)
-   Deploy and test multiple smart contract types
-   Implement transaction spam and mempool fuzzing
-   Validate consensus mechanisms (F3, finalized tipsets)
-   Test network operations and peer management
-   Implement state consistency checks
-   Validate Ethereum compatibility layer
-   Create node health monitoring
-   Implement stress testing capabilities
-   Set up RPC and API testing framework
-   Integrate code coverage instrumentation
-   Create CI jobs for build, push, and testing automation
-   **Integrate Curio storage provider for end-to-end deal testing**
-   **Runtime deployment of Filecoin onchain cloud contracts (USDFC, Warm Storage Service, Service Provider Registry, PDP Verifier)**
-   **Synapse SDK integration for E2E storage service testing**
-   **PDP (Proof of Data Possession) service setup and testing**

### Longer-Term Goals

-   Expand Filecoin Services testing (PDP, FS)
-   Implement fuzz testing for bad inputs
-   Expand Ethereum-based workloads
-   Add more sophisticated consensus testing
-   Expand smart contract testing scenarios
-   Enhanced E2E storage workflow testing

## A Concrete Example

Antithesis has [a public repository that tests ETCD](https://github.com/antithesishq/etcd-test-composer). It serves as a concrete example and a guide for using Test Composer and SDK assertions in various languages. You might find it helpful!
