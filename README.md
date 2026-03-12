# Antithesis Testing with the Filecoin Network

## Purpose

This repository provides a comprehensive testing framework for the Filecoin network using the [Antithesis](https://antithesis.com/) autonomous testing platform. It validates multiple Filecoin implementations (Lotus, Forest, Curio) through deterministic, fault-injected testing.

## Setup Overview

The system runs **9 containers** by default (12 with `--profile foc`):
- **Drand cluster**: `drand0`, `drand1`, `drand2` (randomness beacon)
- **Lotus nodes**: `lotus0`, `lotus1` (Go implementation)
- **Lotus miners**: `lotus-miner0`, `lotus-miner1`
- **Forest node**: `forest0` (Rust implementation)
- **Workload**: Go stress engine container

With `--profile foc` (Filecoin Open Contracts stack):
- **FilWizard**: Contract deployment and environment wiring
- **Curio**: Storage provider with PDP support
- **Yugabyte**: Database for Curio state

## Quick Start

### Prerequisites
- Docker and Docker Compose
- Make

### Build and Run
```bash
# Build all images
make build-all

# Start protocol stack (drand + lotus + forest + workload)
make up

# Start full FOC stack (adds filwizard + curio + yugabyte)
make up-foc

# View logs
make logs

# Stop and cleanup
make cleanup
```

### Available Make Commands
```bash
make help            # Show all commands
make build-all       # Build all images
make build-lotus     # Build Lotus image
make build-forest    # Build Forest image
make build-drand     # Build Drand image
make build-workload  # Build workload image
make build-curio     # Build Curio image
make build-filwizard # Build FilWizard image
make build-infra     # Build infrastructure (drand)
make build-nodes     # Build all node images (lotus, forest, curio)
make up              # Start default services
make up-foc          # Start all + FOC services
make down            # Stop default services
make down-foc        # Stop all services including FOC
make logs            # Follow logs
make restart         # Restart containers
make all             # Build all images and start localnet
make rebuild         # Clean rebuild (down + cleanup + build + up)
make rebuild-foc     # Clean rebuild with FOC profile
make cleanup         # Stop and clean data
make show-versions   # Show image version tags
```

## Stress Engine

The workload container runs a **stress engine** that continuously picks weighted actions ("vectors") and executes them against Lotus and Forest nodes. Each vector uses Antithesis SDK assertions to verify safety and liveness.

The engine runs two different vector decks depending on the profile:
- **Default (filecoin)**: Consensus + mempool + EVM + cross-node + state + reorg vectors
- **FOC (filecoin-foc)**: Consensus + FOC lifecycle + steady-state storage vectors

### Consensus Vectors (always active)

| Vector | Env Var | Description |
|--------|---------|-------------|
| `DoTipsetConsensus` | `STRESS_WEIGHT_TIPSET_CONSENSUS` | Cross-node tipset agreement |
| `DoHeightProgression` | `STRESS_WEIGHT_HEIGHT_PROGRESSION` | Chain height advances |
| `DoPeerCount` | `STRESS_WEIGHT_PEER_COUNT` | Peer connectivity |
| `DoHeadComparison` | `STRESS_WEIGHT_HEAD_COMPARISON` | Cross-node chain head match |
| `DoStateRootComparison` | `STRESS_WEIGHT_STATE_ROOT` | Cross-node state root match |
| `DoStateAudit` | `STRESS_WEIGHT_STATE_AUDIT` | Full state tree audit |

### Mempool Vectors (non-FOC)

| Vector | Env Var | Description |
|--------|---------|-------------|
| `DoTransferMarket` | `STRESS_WEIGHT_TRANSFER` | Random FIL transfers between wallets |
| `DoGasWar` | `STRESS_WEIGHT_GAS_WAR` | Gas premium replacement racing |
| `DoHeavyCompute` | `STRESS_WEIGHT_HEAVY_COMPUTE` | StateCompute re-execution verification |
| `DoAdversarial` | `STRESS_WEIGHT_ADVERSARIAL` | Double-spend, invalid sigs, nonce races |

### FVM/EVM Vectors (non-FOC)

| Vector | Env Var | Description |
|--------|---------|-------------|
| `DoDeployContracts` | `STRESS_WEIGHT_DEPLOY` | Deploy EVM contracts via EAM |
| `DoContractCall` | `STRESS_WEIGHT_CONTRACT_CALL` | Invoke contracts (recursion, delegatecall, tokens) |
| `DoSelfDestructCycle` | `STRESS_WEIGHT_SELFDESTRUCT` | Deploy, destroy, cross-node verify |
| `DoConflictingContractCalls` | `STRESS_WEIGHT_CONTRACT_RACE` | Same-nonce contract calls to different nodes |
| `DoMaxBlockGas` | `STRESS_WEIGHT_MAX_BLOCK_GAS` | Gas limit edge cases |
| `DoLogBlaster` | `STRESS_WEIGHT_LOG_BLASTER` | Excessive event logging |
| `DoMemoryBomb` | `STRESS_WEIGHT_MEMORY_BOMB` | Memory pressure |
| `DoStorageSpam` | `STRESS_WEIGHT_STORAGE_SPAM` | Storage stress |

### Cross-Node Divergence Vectors (non-FOC)

| Vector | Env Var | Description |
|--------|---------|-------------|
| `DoReceiptAudit` | `STRESS_WEIGHT_RECEIPT_AUDIT` | Receipt comparison across nodes |
| `DoMessageOrderingAttack` | `STRESS_WEIGHT_MSG_ORDERING` | Conflicting txs from same sender |
| `DoNonceBombard` | `STRESS_WEIGHT_NONCE_BOMBARD` | Rapid nonce sequences |
| `DoGasExhaustionEdge` | `STRESS_WEIGHT_GAS_EXHAUST` | Gas limit edge cases |

### State Vectors (non-FOC)

| Vector | Env Var | Description |
|--------|---------|-------------|
| `DoActorMigrationStress` | `STRESS_WEIGHT_ACTOR_MIGRATION` | State tree access via deploy/destroy cycles |
| `DoActorLifecycleStress` | `STRESS_WEIGHT_ACTOR_LIFECYCLE` | Actor creation/interaction patterns |

### Network Chaos (non-FOC)

| Vector | Env Var | Description |
|--------|---------|-------------|
| `DoReorgChaos` | `STRESS_WEIGHT_REORG` | Rapid partition, mine, heal cycles |

### FOC Vectors (FOC profile only)

| Vector | Env Var | Description |
|--------|---------|-------------|
| `DoFOCLifecycle` | `STRESS_WEIGHT_FOC_LIFECYCLE` | Sequential state machine (Init through Ready) |
| `DoFOCUploadPiece` | `STRESS_WEIGHT_FOC_UPLOAD` | Upload random data to Curio PDP API |
| `DoFOCAddPieces` | `STRESS_WEIGHT_FOC_ADD_PIECES` | Add pieces to on-chain proofset |
| `DoFOCMonitorProofSet` | `STRESS_WEIGHT_FOC_MONITOR` | Query proofset health + USDFC balances |
| `DoFOCRetrieveAndVerify` | `STRESS_WEIGHT_FOC_RETRIEVE` | Download piece and verify CID |
| `DoFOCTransfer` | `STRESS_WEIGHT_FOC_TRANSFER` | ERC-20 USDFC transfer |
| `DoFOCSettle` | `STRESS_WEIGHT_FOC_SETTLE` | Settle active payment rail |
| `DoFOCWithdraw` | `STRESS_WEIGHT_FOC_WITHDRAW` | Withdraw USDFC from FilecoinPay |
| `DoFOCDeletePiece` | `STRESS_WEIGHT_FOC_DELETE_PIECE` | Schedule piece deletion from proofset (weight 0 default) |
| `DoFOCDeleteDataSet` | `STRESS_WEIGHT_FOC_DELETE_DS` | Delete dataset + reset lifecycle (weight 0 default) |

Weights are configured in `docker-compose.yaml` environment. Set to `0` to disable.

### FOC Sidecar

During FOC runs, a separate `foc-sidecar` process runs alongside the stress engine. It continuously monitors on-chain FOC contract state and emits `assert.Always` safety assertions (e.g. proofset integrity, balance invariants, event consistency). See `workload/FOC.md` for full architecture details.

### Reorg Safety

All state-sensitive assertions use `ChainGetFinalizedTipSet` so they are safe during partition/reorg chaos injected by Antithesis.

## Antithesis Integration

### Fault Injection
Antithesis automatically injects faults (crashes, network partitions, thread pausing) after the workload signals "setup complete".

### SDK Assertions
Test properties use the Antithesis Go SDK:
- `assert.Always()` — Must always hold
- `assert.Sometimes()` — Must hold at least once
- `assert.Reachable()` — Code path must be reached
- `assert.Unreachable()` — Code path must never be reached

### Running Tests on Antithesis
1. Push images to Antithesis registry
2. Use GitHub Actions to trigger tests
3. Review reports in Antithesis dashboard

### GitHub Actions Workflows

#### Build & Push Workflows

Each component has a dedicated build workflow that builds the Docker image and pushes it to the Antithesis GAR registry.

| Workflow | Trigger | Components |
|----------|---------|------------|
| `build_push_lotus.yml` | Nightly (6 PM EST) + Manual | Lotus |
| `build_push_forest.yml` | Nightly (6 PM EST) + Manual | Forest |
| `build_push_curio.yml` | Nightly (6 PM EST) + Manual | Curio |
| `build_push_drand.yml` | Manual | Drand |
| `build_push_workload.yml` | Manual | Workload |
| `build_push_filwizard.yml` | Manual | FilWizard |
| `build_push_config.yml` | Manual | Config |

Scheduled builds fetch the latest commit from the upstream repo and tag the image as `latest`. Manual builds accept a commit hash or tag as input.

#### PR Antithesis Test (`pr_antithesis_test.yml`)

Automatically builds and tests changes on a PR when specific labels are applied:

- **`antithesis-test-filecoin`** — Triggers a test on the `filecoin` endpoint
- **`antithesis-test-foc`** — Triggers a test on the `filecoin-foc` endpoint

The workflow detects which components changed, builds only those images, and triggers a 1-hour ephemeral Antithesis test. Unchanged components use the existing `latest` images from the registry.

#### Run Antithesis Test (`run_antithesis_test.yml`)

Triggers an Antithesis test run. Runs nightly (12-hour runs) for both the Implementors and FOC teams. Can also be triggered manually with custom image tags, duration, endpoint selection, and smoke test flags.

#### Run Antithesis Upgrade Test (`run_antithesis_upgradetest.yml`)

Manual-only workflow for testing image upgrades mid-run. Specify a base set of images, then an upgrade image and tag to swap in during the test.

#### List Registry Images (`list_registry_images.yml`)

Manual-only workflow to list recent image tags in the Antithesis GAR registry. Select a component from the dropdown and the workflow outputs the most recent images with tags, digests, and timestamps. Results appear directly on the workflow run summary page.

#### Get Logs (`get_logs.yml`)

Manual-only workflow to retrieve test logs from Antithesis.

## Directory Structure

```
├── drand/               # Drand beacon build
├── lotus/               # Lotus node build and scripts
├── forest/              # Forest node build and scripts
├── curio/               # Curio storage provider build  [--profile foc]
├── filwizard/           # Contract deployment container [--profile foc]
├── yugabyte/            # YugabyteDB for Curio         [--profile foc]
├── workload/            # Stress engine
│   ├── cmd/
│   │   ├── stress-engine/       # Fuzz driver source
│   │   │   ├── main.go              # Entry point, deck builder, action loop
│   │   │   ├── helpers.go           # Shared message helpers
│   │   │   ├── mempool_vectors.go   # Transfer, gas war, adversarial
│   │   │   ├── evm_vectors.go       # Contract deploy, invoke, selfdestruct, resource stress
│   │   │   ├── consensus_vectors.go # Tipset consensus, height, peers, state roots, audit
│   │   │   ├── crossnode_vectors.go # Receipt audit, msg ordering, nonce bombard
│   │   │   ├── state_vectors.go     # Actor migration, lifecycle stress
│   │   │   ├── reorg_vectors.go     # Partition/mine/heal chaos cycles
│   │   │   ├── foc_vectors.go       # FOC lifecycle + steady-state vectors
│   │   │   └── contracts.go         # EVM bytecodes, ABI encoding
│   │   ├── foc-sidecar/         # Independent FOC safety monitor
│   │   ├── genesis-prep/        # Wallet generation for stress testing
│   │   └── setup-complete/      # Antithesis lifecycle signal utility
│   ├── internal/
│   │   ├── chain/               # RPC client (Lotus + Forest)
│   │   └── foc/                 # FOC contract interaction libraries
│   ├── entrypoint/              # Container startup scripts
│   ├── FOC.md                   # FOC architecture documentation
│   └── Dockerfile
├── scripts/             # Helper scripts (run-local.sh)
├── data/                # Runtime data (git-ignored, created on start)
├── shared/              # Shared configs between containers (git-ignored)
├── versions.env         # Version pins — change to test a new client version
├── Makefile             # Build commands
├── docker-compose.yaml  # Service definitions
└── cleanup.sh           # Data cleanup script
```

## Configuration

### Environment Variables
Located in `.env`:
- Node data directories
- Port configurations
- Shared volume paths

### Version Pinning
Located in `versions.env` — change these to test a specific upstream commit or tag:
```env
# Implementation versions — commit hashes or tags from upstream repos
LOTUS_COMMIT=latest
FOREST_COMMIT=latest
CURIO_COMMIT=latest
DRAND_TAG=latest

# Internal versions — built from this repo
WORKLOAD_TAG=latest
FILWIZARD_TAG=latest
CONFIG_TAG=latest
```



## Documentation

- [Antithesis Documentation](https://antithesis.com/docs/)
- [Lotus Documentation](https://lotus.filecoin.io/)
- [Forest Documentation](https://chainsafe.github.io/forest/)
- [FilWizard](https://github.com/parthshah1/FilWizard) — Contract deployment tool
- [FOC Architecture](workload/FOC.md) — FOC testing design and vectors
