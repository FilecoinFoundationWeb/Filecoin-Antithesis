Antithesis Simulation for Filecoin Network
==========================================

This documentation outlines the structure, purpose, and guidelines for contributing to the Antithesis Simulation project for the Filecoin network. It provides insights into the main idea, the repository's structure, and the current state of the project.

* * * * *

Repository Structure Overview
-----------------------------

The repository contains a containerized setup of a Filecoin localnet for Antithesis Autonomous Testing, including Lotus and Forest nodes patched to work with local Drand nodes. Below is a brief description of the key files and directories:

### Key Files and Directories

-   **cleanup.sh**: Cleans all the persisted data in the localnet setup.
-   **drand/**: Contains the Dockerfile and startup scripts for Drand nodes.
    -   **start_scripts/**: Includes scripts like `drand-1.sh`, `drand-2.sh`, and `drand-3.sh` to start specific Drand nodes.
-   **lotus/**: Similar to Drand but for Lotus. This directory also stores configuration and patch files to support interaction with other nodes.
    -   **start_scripts/**: Contains scripts for starting Lotus nodes and miners (e.g., `lotus-1.sh`, `lotus-miner-1.sh`).
-   **forest/**: Similar to Drand but for Forest nodes. Includes configuration templates and startup scripts.
    -   **start_scripts/**: Scripts like `forest-connector.sh` and `forest-init.sh` to initialize and connect Forest nodes.
-   **workload/**: The main driver of the application, responsible for generating activity chains and asserting test properties. Includes:
    -   **main.go**: Entrypoint of the application, a Go binary with CLI flags for different operations.
    -   **main/**: Contains driver scripts and tests categorized by naming conventions (e.g., `parallel`, `anytime`, `eventually`). These follow test templates from [Antithesis Test Composer Reference](https://antithesis.com/docs/test_templates/test_composer_reference/).
        -   Examples: `parallel_driver_spammer.py`, `anytime_node_height_progression.sh`, `eventually_all_node_sync_status_check.py`.
    -   **resources/**: Helper files for workloads, such as `rpc.py`, `wallets.py`, and smart contract files (`SimpleCoin.sol`).
    -   **removed/**: Contains deprecated or removed tests for reference.

* * * * *

How Antithesis Simulation Testing Works
---------------------------------------

Antithesis Simulation tests distributed systems by generating various failure scenarios and validating system properties under stress. It automates the process of:

1.  Injecting faults (e.g., crashes, network partitions) into the system.
2.  Asserting test properties using the Antithesis SDKs.
3.  Observing system behavior to ensure it meets reliability expectations.

Key components of Antithesis testing include:

-   Fault injection using activity chains.
-   Monitoring system responses.
-   Asserting invariants (e.g., data consistency, fault tolerance).

For more details, refer to the [Antithesis Documentation](https://antithesis.com/docs/introduction/how_antithesis_works/).

* * * * *

Run Antithesis Testing
----------------------

This section provides an overview of the files and goals for running Antithesis testing:


### Steps

To run the localnet stack:

`make forest_commit=<commit_hash> all`

This builds Drand (v2.0.4), Lotus (v1.31.0), and Forest nodes for the specified commit from the [Forest Github repository](https://github.com/ChainSafe/forest). Example:

`make forest_commit=4eefcc25cb66b2d0449979d4d6532f74344f160b all`

For advanced usage, specify different versions of Drand and Lotus:

`make forest_commit=<commit_hash> drand_tag=<drand_version> lotus_tag=<lotus_version> all`

Shutdown and clean up the localnet with:

`make cleanup`

* * * * *

How to Contribute
-----------------

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

* * * * *

Todo
----

### Completed Tasks

-   Implement basic workloads (e.g., Transaction Spammer).
-   Define and implement initial test properties.
-   Integrate code coverage instrumentation.
-   Create CI jobs for build, push, and testing automation.
-   Initialize and manage wallets (create, fund, and delete).
-   Perform randomized transactions between wallets.
-   Upload and invoke smart contracts.
-   Manage node connectivity (connect/disconnect).

### Longer-Term Goals

-   Integrate Curio for enhanced testing.
-   Implement fuzz testing for bad inputs.
-   Expand Ethereum-based workloads.