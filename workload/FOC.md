# FOC (Filecoin On-Chain Cloud) Workload

## Overview

The FOC workload exercises the Filecoin On-Chain Cloud storage protocol end-to-end under [Antithesis](https://antithesis.com/) fault injection. It consists of two binaries that run inside the `workload` Docker container:

- **stress-engine** — drives the FOC lifecycle and fires steady-state test vectors via a weighted random deck
- **foc-sidecar** — independently monitors on-chain state and asserts safety invariants

The FOC protocol involves four smart contracts, a Curio storage provider node, and the Filecoin blockchain:

```
Client Wallet ──► USDFC (ERC-20) ──► FilecoinPay (escrow + payment rails)
                                          │
                                          ▼
                  Curio SP ◄── FWSS (orchestrator) ──► PDPVerifier (proofsets)
                     │
                     ▼
              ServiceProviderRegistry
```

## Why Autonomous Testing?

Contract logic executes deterministically inside FVM's WASM sandbox — unit tests cover that well. What they can't cover is the **distributed system the contracts live inside**:

- **Multi-implementation consensus** — We run 2 Lotus nodes + 1 Forest node. Process crashes, network partitions, and block propagation delays can cause nodes to disagree on tipset ordering, reorg finalized state, or diverge entirely. The sidecar's `assert.Always` invariants against 30-epoch-finalized state catch exactly this class of bug.

- **Cross-contract consistency under concurrency** — Operations like `deposit`, `withdraw`, `settleRail`, and `transfer` modify state across USDFC and FilecoinPay simultaneously. When these land in the same block, get reordered by the mempool, or survive a reorg differently, cross-contract invariants (e.g. solvency: `balanceOf(FilecoinPay) ≥ Σ(funds + lockup)`) can break in ways no isolated test reproduces.

- **Curio crash recovery** — Curio is a stateful off-chain actor: it stores piece data on disk, submits txs on behalf of clients, and responds to on-chain proof challenges. Killing it mid-upload or mid-proof and checking whether proofsets remain live and pieces survive tests its recovery guarantees.

- **Mempool and tx lifecycle** — Nonce gaps from failed submissions, txs accepted but never mined due to node crashes, and concurrent sends from the same wallet all exercise node-level tx management that sits entirely outside contract scope.

## Architecture

### Directory Structure

```
workload/
├── cmd/
│   ├── stress-engine/          # Main fuzz driver
│   │   ├── main.go             # Init, deck building, main loop
│   │   ├── foc_vectors.go      # FOC lifecycle + steady-state vectors
│   │   ├── actions.go          # Non-FOC stress vectors (transfers, contracts, etc.)
│   │   └── contracts.go        # Embedded EVM bytecodes
│   ├── foc-sidecar/            # Independent safety monitor
│   │   ├── main.go             # Polling loop
│   │   ├── assertions.go       # 5 safety assertions (assert.Always)
│   │   ├── events.go           # Event log parsing (DataSetCreated, RailCreated, etc.)
│   │   └── state.go            # Thread-safe state tracking
│   └── genesis-prep/           # Wallet generation (runs before stress-engine)
│       └── main.go
└── internal/
    └── foc/                    # Shared FOC library
        ├── config.go           # Parse /shared/environment.env + SP key
        ├── eth.go              # EVM tx submission (SendEthTx, SendEthTxConfirmed, BuildCalldata)
        ├── eip712.go           # EIP-712 typed data signing for FWSS
        ├── curio.go            # Curio PDP HTTP API client (upload, create dataset, add pieces)
        ├── commp.go            # PieceCIDv2 calculation (CommP)
        └── selectors.go        # ABI function selectors for all contracts
```

### Smart Contracts

| Contract | Role | Key Functions |
|----------|------|--------------|
| **USDFC** | ERC-20 payment token | `approve`, `transfer`, `balanceOf` |
| **FilecoinPayV1** | Escrow + payment rails | `deposit`, `withdraw`, `settleRail`, `setOperatorApproval`, `createRail`, `modifyRailPayment` |
| **FWSS** (FilecoinWarmStorageService) | Orchestrator, EIP-712 signature verification | `terminateService`, `railToDataSet` |
| **PDPVerifier** | Proof-of-Data-Possession proofsets | `createDataSet`, `addPieces`, `schedulePieceDeletions`, `deleteDataSet`, `dataSetLive`, `getActivePieceCount` |
| **ServiceProviderRegistry** | SP registration + capability keys | `addressToProviderId` |

### Wallet Roles

| Role | Source | Purpose |
|------|--------|---------|
| **Client** | `CLIENT_PRIVATE_KEY` in environment.env | Signs all EIP-712 messages, owns USDFC deposits |
| **Deployer** | `DEPLOYER_PRIVATE_KEY` in environment.env | Contract deployer, FWSS owner, initial USDFC holder |
| **SP** | `/var/lib/curio/private_key` (lazy-loaded) | Curio's signing key, registered as service provider |

---

## Lifecycle State Machine

The stress-engine drives the FOC lifecycle through a sequential state machine. Each invocation of `DoFOCLifecycle` advances one step. The lifecycle must reach `Ready` before any steady-state vectors will fire.

```
Init ──► Approved ──► Deposited ──► OperatorApproved ──► DataSetCreated ──► Ready
  │          │            │                │                    │              │
  │          │            │                │                    │              └─ steady-state
  │          │            │                │                    │                 vectors fire
  │          │            │                │                    │
  │          │            │                │                    └─ createDataSet via Curio HTTP
  │          │            │                │                       + EIP-712 client signature
  │          │            │                │
  │          │            │                └─ setOperatorApproval(USDFC, FWSS, true, ...)
  │          │            │                   on FilecoinPay
  │          │            │
  │          │            └─ deposit(USDFC, client, 500 USDFC) on FilecoinPay
  │          │
  │          └─ approve(FilecoinPay, MaxUint256) on USDFC
  │
  └─ (initial state)
```

### Step Details

| Step | Contract | Function | Gas Used | Notes |
|------|----------|----------|----------|-------|
| **Approve** | USDFC | `approve(FilecoinPay, MaxUint256)` | ~5.7M | ERC-20 allowance for FilecoinPay to pull funds |
| **Deposit** | FilecoinPay | `deposit(USDFC, client, 500e18)` | ~22M | Cross-contract `transferFrom` is expensive on FVM |
| **Operator** | FilecoinPay | `setOperatorApproval(USDFC, FWSS, true, ...)` | ~10.9M | Allows FWSS to manage funds on client's behalf |
| **CreateDataSet** | Curio HTTP → PDPVerifier | `createDataSet(FWSS, extraData)` | varies | EIP-712 signed by client, submitted by Curio |
| **WaitForDataSet** | — | — | — | Polls Curio API until on-chain dataset ID is confirmed |

All transactions use EIP-1559 with 30M gas limit and are submitted via `EthSendRawTransaction`.

---

## Steady-State Vectors

Once the lifecycle reaches `Ready`, these vectors fire independently based on their deck weight:

### DoFOCUploadPiece (weight: 2)
Generates random data (128–1024 bytes), computes PieceCIDv2 via CommP, uploads to Curio's 3-step PDP API:
1. `POST /pdp/piece/uploads` → get upload session UUID
2. `PUT /pdp/piece/uploads/{uuid}` → upload raw bytes
3. `POST /pdp/piece/uploads/{uuid}` → finalize with `{"pieceCid": "..."}``

The piece is added to `focState.UploadedPieces` for later on-chain addition.

### DoFOCAddPieces (weight: 1)
Takes one piece from `UploadedPieces`, signs an EIP-712 `AddPieces` message with the client key, and submits via Curio HTTP API (`POST /pdp/data-sets/{id}/pieces`). The CID is decoded from string to binary bytes before signing (critical — the contract verifies against binary CID bytes).

### DoFOCMonitorProofSet (weight: 3)
Reads on-chain state:
- `accounts(USDFC, client)` → funds + lockup from FilecoinPay
- `balanceOf(client)` → USDFC wallet balance
- `dataSetLive(dataSetID)` → proofset liveness
- `getActivePieceCount(dataSetID)` → number of active pieces
- `getNextChallengeEpoch(dataSetID)` → next proving deadline

### DoFOCTransfer (weight: 1)
Transfers a small random amount of USDFC (1–3% of 1 USDFC) from client to deployer wallet.

### DoFOCSettle (weight: 1)
Discovers active payment rails via `getRailsForPayerAndToken`, gets current chain height, and calls `settleRail(railId, currentEpoch)` to trigger payment settlement.

### DoFOCWithdraw (weight: 1)
Reads available funds from FilecoinPay, withdraws 1–5% of available balance back to the client's wallet.

### DoFOCRetrieveAndVerify (weight: 1)
Downloads a random piece from Curio's PDP API (`GET /piece/{cid}`), recomputes PieceCIDv2 via CommP, and verifies the CID matches the original upload. Detects data corruption in the storage/retrieval pipeline.

### DoFOCDeletePiece (weight: 0, opt-in)
Signs EIP-712 `SchedulePieceRemovals` and submits to PDPVerifier. Removes the last added piece from the proofset. **Destructive** — disabled by default.

### DoFOCDeleteDataSet (weight: 0, opt-in)
Two-phase dataset deletion following the FWSS termination pipeline:
1. **Phase 1**: Calls `FWSS.terminateService(clientDataSetId)` to initiate service termination (sets end epoch on the payment rail).
2. **Phase 2** (subsequent invocation): Signs EIP-712 `DeleteDataSet` and submits to `PDPVerifier.deleteDataSet()`. Only succeeds after the termination epoch has passed.

Resets the lifecycle to `Init` on success. **Destructive** — disabled by default.

---

## Assertions

The Antithesis SDK provides three assertion types:
- **`assert.Always`** — Safety property that must **never** be violated. A single failure is a bug.
- **`assert.Sometimes`** — Liveness property that should eventually be true. Under fault injection, any individual attempt can fail, but across the full test run the condition should hold at least once.
- **`assert.Reachable`** — Coverage marker confirming a code path was exercised.

### Stress-Engine Assertions (`foc_vectors.go`)

All stress-engine assertions use `assert.Sometimes` because individual transactions can fail under fault injection — the assertion checks that across the entire test run, the operation succeeds at least once.

| Assertion Message | Type | Vector | What It Validates |
|-------------------|------|--------|-------------------|
| `"USDFC approve for FilecoinPay succeeds"` | Sometimes | DoFOCLifecycle (Approve step) | ERC-20 allowance tx is confirmed on-chain |
| `"USDFC deposit into FilecoinPay succeeds"` | Sometimes | DoFOCLifecycle (Deposit step) | Deposit tx confirmed, funds visible in FilecoinPay |
| `"FWSS operator approval succeeds"` | Sometimes | DoFOCLifecycle (Operator step) | Operator approval tx confirmed on-chain |
| `"FOC dataset creation completes end-to-end"` | Sometimes | DoFOCLifecycle (CreateDataSet step) | Dataset created via Curio HTTP + confirmed on-chain with valid ID |
| `"piece upload to Curio succeeds"` | Sometimes | DoFOCUploadPiece | 3-step Curio PDP upload flow completes successfully |
| `"pieces added to proofset"` | Sometimes | DoFOCAddPieces | EIP-712 signed piece addition confirmed on-chain with piece IDs |
| `"USDFC transfer succeeds"` | Sometimes | DoFOCTransfer | ERC-20 transfer tx accepted by mempool |
| `"payment rail settlement succeeds"` | Sometimes | DoFOCSettle | `settleRail(railId, epoch)` tx accepted by mempool |
| `"USDFC withdrawal from FilecoinPay succeeds"` | Sometimes | DoFOCWithdraw | `withdraw(USDFC, amount)` tx accepted by mempool |
| `"piece deletion scheduled"` | Sometimes | DoFOCDeletePiece | `schedulePieceDeletions` tx accepted by mempool |
| `"FWSS service termination initiated"` | Sometimes | DoFOCDeleteDataSet | `terminateService(clientDataSetId)` confirmed on-chain (phase 1) |
| `"dataset deletion succeeds"` | Sometimes | DoFOCDeleteDataSet | `deleteDataSet` confirmed on-chain after termination epoch passed (phase 2) |
| `"piece retrieval integrity verified"` | Sometimes | DoFOCRetrieveAndVerify | Downloaded piece recomputed CID matches original. Detects data corruption in storage/retrieval. |

### Sidecar Assertions (`assertions.go`)

Sidecar assertions use `assert.Always` for safety invariants that must hold on every poll cycle. These run independently of the stress-engine against finalized chain state (30-epoch finality window).

| Assertion Message | Type | Function | What It Validates |
|-------------------|------|----------|-------------------|
| `"Rail-to-dataset reverse mapping is consistent"` | Always | checkRailToDataset | `railToDataSet(pdpRailId)` returns the expected `dataSetId` for every tracked dataset. Detects rail/dataset mapping corruption. |
| `"FilecoinPay holds sufficient USDFC (solvency)"` | Always | checkFilecoinPaySolvency | `balanceOf(FilecoinPay)` >= sum of all tracked `accounts.funds + accounts.lockup`. Detects insolvency / phantom balance creation. |
| `"Provider ID matches registry for dataset"` | Always | checkProviderIDConsistency | `addressToProviderId(sp)` matches the `providerId` from the `DataSetCreated` event. Detects registry corruption or SP impersonation. |
| `"Active proofset is live on-chain"` | Always | checkProofSetLiveness | Every non-deleted dataset has `dataSetLive() == true`. Detects unexpected dataset termination or proof failure. |
| `"Deleted proofset is not live"` | Always | checkDeletedDataSetNotLive | Every deleted dataset has `dataSetLive() == false`. Detects zombie datasets that survive deletion. |

| `"Proving period advances (challenge epoch changed)"` | Sometimes | checkProvingAdvancement | `getNextChallengeEpoch` changes over time for active datasets. Confirms proving pipeline is running. |
| `"Dataset proof submitted (proven epoch advanced)"` | Sometimes | checkProvingAdvancement | `getDataSetLastProvenEpoch` advances. Confirms Curio is submitting proofs. |
| `"Active piece count does not exceed leaf count"` | Always | checkPieceAccountingConsistency | `getActivePieceCount <= getDataSetLeafCount`. Detects piece accounting corruption. |
| `"Active dataset rail has non-zero payment rate"` | Always | checkRateConsistency | Datasets with pieces must have `paymentRate > 0` on their PDP rail. Detects rate miscalculation. |

### Event Tracking

The sidecar monitors these on-chain events to build its state:
- **DataSetCreated** (from FWSS) — tracks datasets with their rail IDs, provider IDs, payers
- **DataSetDeleted** (from PDPVerifier) — marks datasets as deleted
- **RailCreated** (from FilecoinPay) — tracks payment rails with token, payer, payee

---

## Shared Library (`internal/foc/`)

### `eth.go` — EVM Transaction Submission

```go
// Build ABI calldata from selector + encoded args
foc.BuildCalldata(foc.SigDeposit, foc.EncodeAddress(token), foc.EncodeAddress(owner), foc.EncodeBigInt(amount))

// Fire-and-forget (best-effort receipt check)
foc.SendEthTx(ctx, node, privKey, toAddr, calldata, "tag")

// Wait for receipt, return true only on status=1
foc.SendEthTxConfirmed(ctx, node, privKey, toAddr, calldata, "tag")

// Read-only calls
foc.EthCallUint256(ctx, node, to, calldata)  // decode uint256
foc.EthCallBool(ctx, node, to, calldata)     // decode bool
foc.EthCallRaw(ctx, node, to, calldata)      // raw bytes
```

All transactions use:
- ChainID: `31415926` (devnet)
- GasLimit: `30,000,000` (FVM cross-contract calls are expensive)
- MaxFeePerGas: `1 nanoFIL`
- Local nonce cache with invalidation on send failure or receipt timeout

### `eip712.go` — EIP-712 Typed Data Signing

Signs messages for FWSS contract operations. Domain separator:
- name: `"FilecoinWarmStorageService"`
- version: `"1"`
- chainId: `31415926`
- verifyingContract: FWSS proxy address

Supported message types:
- `CreateDataSet(clientDataSetId, payee, metadata[])`
- `AddPieces(clientDataSetId, nonce, pieceData[], pieceMetadata[])`
- `SchedulePieceRemovals(clientDataSetId, pieceIds[])`
- `DeleteDataSet(clientDataSetId)`

### `curio.go` — Curio PDP HTTP Client

| Function | Endpoint | Purpose |
|----------|----------|---------|
| `PingCurio` | `GET /pdp/ping` | Health check |
| `UploadPiece` | 3-step flow (see above) | Upload raw data |
| `FindPiece` / `WaitForPiece` | `GET /pdp/piece?pieceCid=...` | Check piece indexing |
| `CreateDataSetHTTP` | `POST /pdp/data-sets` | Create dataset |
| `WaitForDataSetCreation` | `GET /pdp/data-sets/created/{txHash}` | Poll until confirmed |
| `AddPiecesHTTP` | `POST /pdp/data-sets/{id}/pieces` | Add pieces to dataset |
| `WaitForPieceAddition` | `GET /pdp/data-sets/{id}/pieces/added/{txHash}` | Poll until confirmed |
| `GetDataSet` | `GET /pdp/data-sets/{id}` | Read dataset info |
| `DownloadPiece` | `GET /piece/{cid}` | Download piece data |

### `config.go` — Environment Parsing

Reads `/shared/environment.env` (written by filwizard during setup) for contract addresses and wallet keys. The SP key is loaded separately from `/var/lib/curio/private_key` (written by Curio init), with lazy retry since Curio may start after the workload.

### `commp.go` — PieceCIDv2 Calculation

Computes the Filecoin piece commitment (CommP) using `go-commp-utils` and encodes it as a PieceCIDv2 per FRC-0069:
- Digest format: `[padding varint][height byte][root 32 bytes]`
- Multihash code: `0x1011` (fr32-sha2-256-trunc254-padded-binary-tree)
- CID codec: `0x55` (raw)

---

## Configuration

All configuration is via environment variables in `docker-compose.yaml`:

### General

| Variable | Default | Description |
|----------|---------|-------------|
| `STRESS_NODES` | `lotus0` | Comma-separated list of Lotus/Forest node hostnames |
| `STRESS_RPC_PORT` | `1234` | Lotus JSON-RPC port |
| `STRESS_FOREST_RPC_PORT` | `3456` | Forest JSON-RPC port |
| `STRESS_KEYSTORE_PATH` | `/shared/configs/stress_keystore.json` | Path to wallet keystore |
| `STRESS_WAIT_HEIGHT` | `10` | Minimum chain height before starting |
| `CURIO_PDP_URL` | `http://curio:80` | Curio PDP API base URL |
| `STRESS_DEBUG` | `0` | Enable verbose debug logging |

### Deck Weights

Each `STRESS_WEIGHT_*` variable controls how many times that action appears in the weighted deck. Higher weight = selected more frequently. Weight `0` disables the action.

When the FOC profile is active, non-FOC stress vectors (EVM contracts, nonce chaos, etc.) are auto-skipped. The deck contains only consensus health checks and FOC vectors.

**FOC vectors** (requires `foc` compose profile):

| Variable | Default | Category | Description |
|----------|---------|----------|-------------|
| `STRESS_WEIGHT_FOC_LIFECYCLE` | `6` | Setup | Drives state machine: Init → ... → Ready |
| `STRESS_WEIGHT_FOC_UPLOAD` | `4` | Steady-state | Upload random data to Curio PDP API |
| `STRESS_WEIGHT_FOC_ADD_PIECES` | `3` | Steady-state | Add uploaded pieces to on-chain proofset |
| `STRESS_WEIGHT_FOC_MONITOR` | `4` | Steady-state | Query proofset health + USDFC balances |
| `STRESS_WEIGHT_FOC_RETRIEVE` | `2` | Steady-state | Download piece and verify CID integrity |
| `STRESS_WEIGHT_FOC_TRANSFER` | `2` | Steady-state | ERC-20 USDFC transfer (client → deployer) |
| `STRESS_WEIGHT_FOC_SETTLE` | `2` | Steady-state | Settle active payment rail |
| `STRESS_WEIGHT_FOC_WITHDRAW` | `2` | Steady-state | Withdraw USDFC from FilecoinPay |
| `STRESS_WEIGHT_FOC_DELETE_PIECE` | `1` | Destructive | Schedule piece deletion from proofset |
| `STRESS_WEIGHT_FOC_DELETE_DS` | `0` | Destructive | Delete entire dataset + reset lifecycle |

---

## Running

### Start FOC devnet

```bash
docker compose --profile foc up -d
```

This starts: drand (3 nodes), lotus (2 nodes), forest (1 node), filwizard, yugabyte, curio, and workload.

### Monitor logs

```bash
# Lifecycle progress
docker logs workload 2>&1 | grep '\[foc-lifecycle\]'

# Piece uploads and additions
docker logs workload 2>&1 | grep '\[foc-upload\]\|\[foc-add-pieces\]'

# Sidecar assertions
docker logs workload 2>&1 | grep '\[foc-sidecar\]'

# Safety violations (should never appear)
docker logs workload 2>&1 | grep 'VIOLATION'

# Overall progress summary
docker logs workload 2>&1 | grep '\[foc-progress\]'
```

### Build workload binary locally

```bash
cd workload
go build ./cmd/stress-engine
go build ./cmd/foc-sidecar
go vet ./...
```

---

## Key Design Decisions

1. **Flat architecture** — No interfaces, no dependency injection. Global state with mutex protection. This matches the Antithesis testing model where simplicity aids reproducibility.

2. **Local signing** — All transactions are signed locally using raw secp256k1 keys and submitted via `EthSendRawTransaction`. No node-side wallet operations.

3. **Weighted random deck** — Actions are selected randomly with Antithesis deterministic RNG. Weights control frequency, not ordering. The lifecycle state machine handles ordering internally.

4. **Fire-and-forget vs confirmed** — Lifecycle steps use `SendEthTxConfirmed` (blocks until receipt). Steady-state vectors use `SendEthTx` (best-effort receipt check) to avoid blocking the deck.

5. **Sidecar independence** — Safety assertions run in a separate polling loop, not in the stress-engine's hot path. This ensures invariants are checked even under high load or engine failures.

6. **30M gas limit** — FVM cross-contract EVM calls have significantly higher gas costs than native EVM. The deposit step alone uses ~22M gas due to `transferFrom` crossing contract boundaries.

7. **Vector isolation** — When FOC is active, non-FOC stress vectors are auto-skipped so FOC vectors aren't diluted. Consensus health checks always run.

---

## Future Work

- **SP-to-SP piece pull (`/pull` flow)** — Curio supports `POST /pdp/piece/pull` for one SP to pull data from another, which is the core multi-copy/durability mechanism. Testing this requires a second Curio node (with its own Yugabyte, SP registration, and PDP wallet). Planned for when multi-Curio devnet support is added.
- **`depositWithPermitAndApproveOperator`** — Combined deposit + operator approval in a single tx (the production flow). Requires EIP-2612 permit support in MockUSDFC.
- **Session key testing** — `SessionKeyRegistry` enables delegated signing. Not yet exercised.
- **Larger piece sizes (40+ MiB)** — Curio caches proof data above ~40 MiB, exercising different code paths. Currently limited to keep devnet test cycles fast.
- **`addPieces` with `dataSetId=0`** — Production flow creates datasets along with the first piece. The separate `createDataSet` path may be removed upstream.
