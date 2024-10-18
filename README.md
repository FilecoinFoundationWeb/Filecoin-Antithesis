## Introduction

This repository contains a (docker) containerized setup of a Filecoin localnet for Antithesis Autonomous Testing. The localnet currently consists of Lotus and Forest nodes and is patched to work with local Drand nodes (vs. live Drand API endpoints)

## How does Antithesis Simulation Testing Work

(TBA)

## Run Antithesis Testing

(TBD) - Waiting for Github Action for Triggering

## View Recent Test Results

(TBA) - SLACK channel integration
(TBA) - Dashboard

## How to run the localnet (for sanity check)

The simplest way to run the localnet stack is to run the:

`make forest_commit=<commit_hash> all`

You can find the forest commit hash on the [Forest github](https://github.com/ChainSafe/forest). For example: `4eefcc25cb66b2d0449979d4d6532f74344f160b` 

1. This will build Drand (v2.0.4), Lotus (v1.29.1) and a Forest node of the corresponding commit passed into the make target.

2. It will then attempt to run a `docker-compose up` command with the newly built container images

You can observe the localnet chain moving and miners performing work. To shutdown the localnet and clean up, run:

`make cleanup`

## Advanced usage

You can pass in additional argument to the make target to run different Drand/Lotus version. For example:

```
make forest_commit=4eefcc25cb66b2d0449979d4d6532f74344f160b \
drand_tag=v2.0.3 \
lotus_tag=v1.29.0 \
all
```

There are also individual build targets for different tasks such as building container images or running the `docker-compose up` commands specifically.

## How to contribute

* Iterating on the workload (tests)
* Define and implement test properties via Antithesis SDKs
* Improve the setup of the localnet
* Devops and CI integration
* Documentation 

## Todo

**Immediate**

* Basic workload implementation (Transaction Spammer)
* Initial Test property implementation
* Create code coverage instrumentation 
* Creating CI jobs to automate build, push and testing

**Longer term**

* Create and submit a pull request for Forest that removes the dependency for live Drand API endpoint
* Create and submit a pull request for Lotus that removes the dependency for live Drand API endpoint