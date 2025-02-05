# Antithesis Testing with the Filecoin Network

## Purpose

This README serves as a guide for both prospective and active contributers. We will walk through the setup structure, using Antithesis, best practices for contributions, and how to interpret the reports. You should be able to walk away with the purpose of continuous testing, why it is important, the current state of this project, and hopefully ideas to contribute!

## Setup

There are 10 containers running in this system: 3 make up a drand cluster (`drand-1`, `drand-2`, `drand-3`), 2 lotus and 2 forest nodes, 2 lotus miners, and 1 `workload` that ["makes the system go"](https://antithesis.com/docs/getting_started/basic_test_hookup/).

The `workload` container has the test scripts where endpoints are called, smart contracts deployed, transactions requested, etc... There are also validations to assert correctness and guarantees also occur in this container using the [Antithesis SDK](https://antithesis.com/docs/using_antithesis/sdk/). We explain more on the SDK in a later section.

## Github Files and Directories

The repository has the containerized setup explained above. Note: Antithesis is fully deterministic and requires our SUT to run without internet access (it is source of nondeterminism). We've made small patches for the Lotus and Forest nodes to work with a local Drand cluster.

Below is a brief description of the key files and directories:

-   **drand/**: Contains the Dockerfile and startup scripts for Drand nodes.
    -   **start_scripts/**: Includes scripts like `drand-1.sh`, `drand-2.sh`, and `drand-3.sh` to start specific Drand nodes.

-   **forest/**: Contains the Dockerfile and startup scripts for the forest node. Also includes configuration templates.
    -   **start_scripts/**: The `forest-init.sh` startup script will initialize a Forest node. The `forest-connector.sh`will connect a Forest node.

-   **lotus/**: Contains the Dockerfile and startup scripts for the lotus node. Also includes configuration and patch files to support interaction with other nodes.
    -   **start_scripts/**: Contains scripts for starting Lotus nodes and miners (e.g., `lotus-1.sh`, `lotus-miner-1.sh`).

-   **workload/**: The main driver of the application, responsible for generating activity chains and asserting test properties. Includes:
    -   **main.go**: Entrypoint of the application, a Go binary with CLI flags for different operations.
    -   **main/**: Contains driver scripts and tests categorized by naming conventions (e.g., `parallel`, `anytime`, `eventually`). These are [Test Composer](https://antithesis.com/docs/test_templates/) commands that enable Antithesis to generate thousands of test cases that will run over a multitude of system states. Test Composer handles varying things like parallelism, test length, and command order.
        -   Examples: `parallel_driver_spammer.py`, `anytime_node_height_progression.sh`, `eventually_all_node_sync_status_check.py`.
    -   **resources/**: Helper files for workloads, such as `rpc.py`, `wallets.py`, and smart contract files (`SimpleCoin.sol`).
    -   **removed/**: Contains deprecated or removed tests for reference.
    -   **go-test-scripts/**: Contains more Go tests. They are called from an executable in main. Allows for Test Composer to control these scripts.

-   **cleanup.sh**: Cleans the persisted data in a local setup

## Using Antithesis

Antithesis is an autonomous testing platform that finds the bugs in your software, with perfect reproducibility to help you fix them.

### Antithesis Fault Injector

Antithesis generates various failure scenarios. The FileCoin system should be resilient to these faults since they happen in production! We automate the process of injecting faults (e.g., crashes, network partitions, thread pausing) into the system, as well as observing system metrics like unexpected container exits and memory usage.

Note: Faults are not injected into the SUT until a "setup_complete" message is emitted. This message is emitted from the `entrypoint.py` script in the `workload` container.

### Antithesis SDK & Test Properties

To generate test cases, Antithesis relies on **test properties** you define. This short video walks through defining SDK assertions within the `workload` container. Assertions can defined in any container in the SUT.

[SDK & Test Properties Video](https://drive.google.com/file/d/1x5VbelH-0WmMvIV4u8vWOgR046A0oubq/view?usp=drive_link)

### Triaging the Report and viewing your Test Properties

Triaging the reports is critical to determine if any of your test properties failed. This short video walks through the report test properties and how they relate to the assertions defined in the `workload` container.

[Triage the Report Video](https://drive.google.com/file/d/1ESQRLXBJitEv9H5e0mcAe6yWiylu0MPd/view?usp=drive_link)

### Running an Antithesis Test from GitHub

To run a manual Antithesis Test, we have implemented GitHub actions. There is also a cron job for nightly 10 hour runs. This short video explains how to run these actions with the branch your test properties are defined on.

[GitHub Actions Video](https://drive.google.com/file/d/1dFBuBnVcFcE-vSFsnIh-jcSVY5m9ALQK/view?usp=drive_link)

### Antithesis Test Composer

[The Antithesis Test Composer](https://antithesis.com/docs/test_templates/first_test/) is a framework that gives the Antithesis system control over what is being executed. Hundreds of thousands of different scenarios are executed during a long enough test 

[Test Composer Video](https://drive.google.com/file/d/1MLk_NAVMfq5BsBT_DPkiksqh5oSQpB2m/view?usp=drive_link)

For more details, refer to the [Antithesis Documentation](https://antithesis.com/docs/introduction/how_antithesis_works/).

## Sanity Check Locally

A good practice to confirm your test script works correctly in Antithesis is to run it locally. Here are the steps:

1. Build each image required by the docker-compose.yml. We need a total of 4 images (`lotus:latest`, `forest:latest`, `drand:latest`, `workload:latest`). Below is an example of building the lotus image while inside the lotus directory.

`docker build . lotus:latest`

2. Run `docker-compose up` from the root directory to start all containers defined in `docker-compose.yml`

3. After the workload container has signaled `setupComplete` (or printed `system is healthy`), you can run any test command 1 to many times via `docker exec`:

`docker exec workload /opt/antithesis/test/v1/main/parallel_driver_create_wallets.sh`

4. We should see the command successfully complete. You've now validated that your test is ready to run on the Antithesis platform! (Note that SDK assertions won't be evaluated locally).

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

## Todo

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

## A Concrete Example

Antithesis has [a public repository that tests ETCD](https://github.com/antithesishq/etcd-test-composer). It serves as a concrete example and a guide for using Test Composer and SDK assertions in various languages. You might find it helpful!
