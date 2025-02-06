# Antithesis Testing with the Filecoin Network

## Purpose

This README serves as a guide for both prospective and active contributers. We will walk through the setup structure, using Antithesis, best practices for contributions, and how to interpret the reports. You should be able to walk away with the purpose of continuous testing, why it is important, the current state of this project, and hopefully ideas to contribute!

## Setup

There are 10 containers running in this system: 3 make up a drand cluster (`drand-1`, `drand-2`, `drand-3`), 2 lotus nodes (`lotus-1`, `lotus-2`), 2 forest nodes (`forest`, `forest-connector`), 2 lotus miners (`lotus-miner-1`, `lotus-miner-2`), and 1 `workload` that ["makes the system go"](https://antithesis.com/docs/getting_started/basic_test_hookup/).

The `workload` container has the [test commands](https://antithesis.com/docs/test_templates/first_test/#test-commands) where endpoints are called, smart contracts deployed, transactions requested, etc... There are also validations to assert correctness and guarantees also occur in this container using the [Antithesis SDK](https://antithesis.com/docs/using_antithesis/sdk/). We explain more on the SDK in a later section.

## Github Files and Directories

In this repository, we have directories that build all the images referenced in the setup above. 

We've made small patches for the Lotus and Forest nodes to work with a local Drand cluster. Antithesis is fully deterministic and requires our SUT to run without internet access (a source of nondeterminism).

The `cleanup.sh` executable will clear the data directory. This data directory is used when running the docker-compose locally, so emptying this is necessary after shutting down the system.

There are supplementary READMEs located in the drand, forest, lotus, and workload directories. These provide some description specific to their respective folders. 

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

[The Antithesis Test Composer](https://antithesis.com/docs/test_templates/first_test/) is a framework that gives the Antithesis system control over what is being executed. Hundreds of thousands of different scenarios are executed during a long enough test. It looks for executables with a specific naming convention in a specific directory (explained in the video below).

[Test Composer Video](https://drive.google.com/file/d/1MLk_NAVMfq5BsBT_DPkiksqh5oSQpB2m/view?usp=drive_link)

For more details, refer to the [Antithesis Documentation](https://antithesis.com/docs/introduction/how_antithesis_works/).

## Sanity Check Locally

A good practice to confirm your test script works correctly in Antithesis is to run it locally. Here are the steps:

1. Build each image required by the docker-compose.yml. We need a total of 4 images (`lotus:latest`, `forest:latest`, `drand:latest`, `workload:latest`). Below is an example of building the lotus image while inside the lotus directory.

`docker build . lotus:latest`

2. Run `docker-compose up` from the root directory to start all containers defined in `docker-compose.yml`

3. After the workload container has signaled `setupComplete` (or printed `system is healthy`), you can run any test command 1 to many times via `docker exec`:

`docker exec workload /opt/antithesis/test/v1/main/parallel_driver_create_wallets.sh`

4. We should see the command successfully complete. You've now validated this test is ready to run on the Antithesis platform! (Note that SDK assertions won't be evaluated locally).

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
