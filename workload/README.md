# Filecoin Antithesis Workload

This directory contains the **stress engine** for validating Filecoin nodes (Lotus, Forest) using the [Antithesis](https://antithesis.com/) testing platform.

## Architecture

The stress engine runs as a continuous loop, randomly picking weighted actions ("vectors") from a deck and executing them against the connected Filecoin nodes. Each vector targets a specific subsystem and uses Antithesis SDK assertions to verify safety and liveness properties.

```
entrypoint.sh → stress-engine binary
  ├── Connects to lotus0, lotus1, forest0 via JSON-RPC
  ├── Loads pre-funded wallets from shared keystore
  ├── Initializes EVM contract bytecodes
  └── Runs weighted action loop (pick → execute → assert)
```

## Stress Vectors

### Mempool & Transfers (`mempool_vectors.go`)

| Vector | Env Var | Description |
|--------|---------|-------------|
| `DoTransferMarket` | `STRESS_WEIGHT_TRANSFER` | Random FIL transfers between wallets via random nodes |
| `DoGasWar` | `STRESS_WEIGHT_GAS_WAR` | Mempool replacement: low-premium tx followed by same-nonce high-premium tx |
| `DoAdversarial` | `STRESS_WEIGHT_ADVERSARIAL` | Double-spend races, invalid signatures, nonce races across nodes |

### EVM/FVM Contracts (`evm_vectors.go`)

| Vector | Env Var | Description |
|--------|---------|-------------|
| `DoDeployContracts` | `STRESS_WEIGHT_DEPLOY` | Deploy EVM contracts (recursive, delegatecall, simplecoin, selfdestruct, extrecursive) via EAM |
| `DoContractCall` | `STRESS_WEIGHT_CONTRACT_CALL` | Invoke deployed contracts: deep recursion, delegatecall, token transfer, external calls |
| `DoSelfDestructCycle` | `STRESS_WEIGHT_SELFDESTRUCT` | Deploy → destroy → cross-node state verification |
| `DoConflictingContractCalls` | `STRESS_WEIGHT_CONTRACT_RACE` | Same-nonce conflicting contract calls to different nodes |

### Consensus & Node Health (`consensus_vectors.go`)

| Vector | Env Var | Description |
|--------|---------|-------------|
| `DoHeavyCompute` | `STRESS_WEIGHT_HEAVY_COMPUTE` | Re-execute `StateCompute` for recent epochs, verify roots match |
| `DoChainMonitor` | `STRESS_WEIGHT_CHAIN_MONITOR` | 6 sub-checks (see below) |

#### DoChainMonitor Sub-checks

All state-sensitive checks use `ChainGetFinalizedTipSet` to avoid false positives during partition → reorg chaos.

| Sub-check | What it verifies |
|-----------|-----------------|
| `tipset-consensus` | All nodes agree on tipset at a finalized height |
| `height-progression` | All node heights within 10 epochs of each other |
| `peer-count` | Every node has ≥1 peer |
| `head-comparison` | Finalized tipset keys match across nodes |
| `state-root-comparison` | Parent state roots match at finalized height |
| `state-audit` | State roots + parent messages/receipts match at finalized height |

## Configuration

Weights are set via environment variables in `docker-compose.yaml`. Set a weight to `0` to disable a vector. Default weights are defined in `main.go:buildDeck()`.

```yaml
environment:
  - STRESS_WEIGHT_TRANSFER=10
  - STRESS_WEIGHT_GAS_WAR=2
  - STRESS_WEIGHT_ADVERSARIAL=2
  - STRESS_WEIGHT_HEAVY_COMPUTE=1
  - STRESS_WEIGHT_CHAIN_MONITOR=3
  - STRESS_WEIGHT_DEPLOY=5
  - STRESS_WEIGHT_CONTRACT_CALL=3
  - STRESS_WEIGHT_SELFDESTRUCT=1
  - STRESS_WEIGHT_CONTRACT_RACE=2
```

Additional config:
- `STRESS_NODES` — Comma-separated node names (e.g., `lotus0,lotus1`)
- `STRESS_RPC_PORT` — RPC port for Lotus nodes (default `1234`)
- `STRESS_KEYSTORE_PATH` — Path to pre-funded wallet keystore
- `STRESS_WAIT_HEIGHT` — Block height to wait for before starting

## Source Files

```
workload/cmd/stress-engine/
├── main.go               # Entry point, deck builder, action loop
├── helpers.go            # Shared: baseMsg, signMsg, pushMsg, nodeType
├── mempool_vectors.go    # Transfer, gas war, adversarial vectors
├── evm_vectors.go        # Contract deploy, invoke, selfdestruct, race
├── consensus_vectors.go  # Heavy compute, chain monitor (6 sub-checks)
└── contracts.go          # EVM bytecodes, deploy/invoke helpers, ABI encoding
```

## Building

```bash
cd workload
docker build -t workload:test .
```

## Assertions

Uses Antithesis SDK assertions:
```go
assert.Always(condition, "id", details)    // Must always hold (safety)
assert.Sometimes(condition, "id", details) // Must hold at least once (liveness)
```

Key assertion IDs:
- `tipset_consensus` — Nodes agree on finalized tipsets
- `cross_node_state_consistent` — State roots match at finalized heights
- `state_root_post_fvm_consistent` — FVM execution produces same state
- `invalid_signature_rejected` — Bad signatures are always rejected
- `contract_deployed` — Contracts are successfully deployed
- `state_audit_verified` — Messages/receipts consistent across nodes
