# Protocol Fuzzer

A libp2p-level protocol fuzzer for Filecoin nodes (Lotus and Forest). It creates ephemeral libp2p peers and sends malformed data through GossipSub, ChainExchange, and Hello protocols to test crash resilience, resource limits, and input validation.

Runs inside [Antithesis](https://antithesis.com) for deterministic, reproducible testing.

## Attack Categories

### `oom/*` — CBOR Length-Prefix OOM (`cbor_bombs.go`)

CBOR encodes collection sizes in the header byte. For example, an array of 5 elements starts with a header saying "array, length 5". Decoders read this header and **pre-allocate** before reading the actual elements:

```
make([]cid.Cid, headerLength)   // Go (cbor-gen)
Vec::with_capacity(header_len)  // Rust (serde_cbor)
```

If an attacker writes a header claiming **1 billion elements** but provides zero actual data, the decoder allocates ~8–40GB before discovering the mismatch. The OS OOM killer terminates the process.

These vectors craft otherwise-valid Filecoin wire messages where **one specific field** has a falsified CBOR length header, then publish them via GossipSub.

| Vector | Target | What Happens |
|--------|--------|-------------|
| `oom/header-parents` | BlockHeader.Parents (CID array) | `make([]cid.Cid, 1B)` ≈ 40GB |
| `oom/header-beacon-entries` | BlockHeader.BeaconEntries (struct array) | `make([]BeaconEntry, 1B)` |
| `oom/header-winpost-proof` | BlockHeader.WinPoStProof (struct array) | `make([]PoStProof, 1B)` |
| `oom/blockmsg-bls-cids` | BlockMsg.BlsMessages (CID array) | `make([]cid.Cid, 1B)` ≈ 40GB |
| `oom/blockmsg-secpk-cids` | BlockMsg.SecpkMessages (CID array) | `make([]cid.Cid, 1B)` ≈ 40GB |
| `oom/signedmsg-params` | Message.Params (raw bytes) | `make([]byte, 1B)` = 1GB |
| `oom/signedmsg-signature` | SignedMessage.Signature (raw bytes) | `make([]byte, 1B)` = 1GB |

### `stack/*` — Stack Exhaustion (`cbor_bombs.go`)

CBOR allows arbitrary nesting: array-of-array-of-array. If the decoder uses recursion (as cbor-gen does), 100–200 levels of nesting exhausts the call stack.

| Vector | What It Does |
|--------|-------------|
| `stack/deeply-nested-cbor` | 100-200 levels of nested arrays in BlockHeader.BeaconEntries field |

### `f3/*` — F3/GPBFT Message Fuzzing (`f3_attacks.go`)

F3 (Fast Finality via GPBFT) is Filecoin's newest consensus subsystem. GMessage payloads are ZSTD-compressed CBOR published to `/f3/2k`. These vectors test the F3 message parsing and validation pipeline.

| Vector | What It Tests |
|--------|-------------|
| `f3/gpbft-zero-value-message` | All fields zero — tests zero-value handling in state machine |
| `f3/gpbft-uint64-overflow` | MaxUint64 for Sender, Instance, Round — arithmetic overflow in comparisons |
| `f3/gpbft-invalid-phase-step` | Step values 5-255 (valid: 0-4) — unguarded match/switch arms |
| `f3/gpbft-empty-ecchain` | Empty ECChain array — zero-length vote handling |
| `f3/gpbft-oversized-ecchain` | 128+ TipSets × 760B keys — near max payload, memory pressure |
| `f3/gpbft-truncated-bls-sig` | Signature shorter than 96 bytes — BLS verification on short input |
| `f3/gpbft-oversized-bls-sig` | Signature longer than 96 bytes — bounds checking on BLS input |
| `f3/gpbft-nil-fields` | Random fields set to CBOR null — nil pointer dereference paths |
| `f3/gpbft-signer-bitfield-oom` | Justification Signers bitfield claiming 2^32 entries — OOM via bitfield allocation |
| `f3/gpbft-bitflip-mutation` | Valid message with 1-5 random bit flips — finds decode/encode asymmetry |
| `f3/gpbft-epoch-overflow` | TipSet epoch = MaxUint64 — epoch arithmetic overflow |
| `f3/gpbft-malformed-cbor` | Truncated, junk-appended, type-confused CBOR — parser robustness |

### `gossip/*` — GossipSub Payload Attacks (`gossip_attacks.go`)

Malformed messages published to `/fil/blocks/` and `/fil/msgs/` topics targeting the message validation pipeline.

| Vector | What It Tests |
|--------|-------------|
| `gossip/block-null-header` | BlockMsg with Header=null |
| `gossip/block-nil-ticket` | Header with Ticket=nil |
| `gossip/block-bad-address` | Delegated address (protocol 0x04) with invalid sub-address |
| `gossip/msg-bad-address` | Same address attack on SignedMessage |
| `gossip/msg-addr-roundtrip` | Systematic address fuzzing across all protocol types |
| `gossip/block-addr-roundtrip` | Same for block Miner addresses |
| `gossip/msg-addr-bitflip` | Surgical bit flips in address bytes of valid SignedMessage |
| `gossip/block-addr-bitflip` | Surgical bit flips in Miner address of valid BlockMsg |
| `gossip/msg-bigint-edge` | BigInt edge cases (negative sign, max uint64, empty) in gas/value fields |

### `exchange/*` — ChainExchange Server Attacks (`exchange_server.go`)

Fuzzer acts as a malicious ChainExchange server. It connects to a target, sends a Hello claiming a heavier chain, then serves mutated data when the victim fetches.

| Vector | What It Tests |
|--------|-------------|
| `exchange/poison-block-duplicate-cid` | Plant block with nil ticket, request it with duplicate CIDs → server-side `NewTipSet` panic |
| `exchange/nil-secpk-message` | Serve nil entry in CompactedMessages.Secpk → `.Cid()` on nil panics |
| `exchange/random-nil-fields` | Combinatorial nil fields + OOB include indices in ChainExchange responses |

### `libp2p/*` — Connection & Stream Chaos (`chaos_driver.go`)

Tests libp2p resource management, connection handling, and stream lifecycle at the transport level.

| Vector | What It Tests |
|--------|-------------|
| `libp2p/rapid-connect-disconnect` | 30-70 rapid connect/disconnect cycles — FD exhaustion, peer manager cleanup |
| `libp2p/stream-exhaustion` | 200-500 streams opened without reading or writing — stream limit enforcement |
| `libp2p/slow-read-backpressure` | Read ChainExchange response at 1 byte/sec — write timeout, goroutine leak |
| `libp2p/peer-identity-flood` | 50-100 simultaneous connections from unique peers — peer table limits |
| `libp2p/half-open-streams` | 30-60 streams with partial garbage data, never closed — resource cleanup |
| `libp2p/bogus-protocol-negotiation` | Invalid/oversized/binary protocol IDs — multistream-select handler robustness |

## Configuration

All category weights are configurable via environment variables. A weight of 0 disables the category. Higher weight = more frequent selection from the deck.

| Variable | Default | Description |
|----------|---------|-------------|
| `FUZZER_ENABLED` | `1` | Set to `0` to disable the fuzzer entirely |
| `FUZZER_WEIGHT_CBOR_BOMBS` | `4` | CBOR length-prefix OOM + stack exhaustion vectors |
| `FUZZER_WEIGHT_F3` | `4` | F3/GPBFT message fuzzing vectors |
| `FUZZER_WEIGHT_CHAOS` | `2` | libp2p connection/stream chaos vectors |
| `FUZZER_WEIGHT_GOSSIP` | `3` | GossipSub payload attack vectors |
| `FUZZER_WEIGHT_EXCHANGE_SERVER` | `3` | ChainExchange server-side attack vectors |
| `FUZZER_RATE_MS` | `500` | Milliseconds between attack iterations |
| `FUZZER_IDENTITY_POOL_SIZE` | `20` | Max ephemeral libp2p hosts in pool |
| `FUZZER_DEBUG` | `0` | Set to `1` for verbose per-vector logging |
| `STRESS_NODES` | `lotus0` | Comma-separated target node names (e.g., `lotus0,lotus1,forest0`) |

## Adding New Vectors

1. Write your attack function in the appropriate `*_attacks.go` file (or create a new one)
2. Add it to the `getAll*Attacks() []namedAttack` function with a descriptive `category/name` format
3. If it's a new category, add a `weightedCategory` entry in `buildDeck()` in `main.go`
4. Use `publishGossipPayload(topicName, data)` for GossipSub-based attacks
5. Use `pool.GetFresh(ctx)` for attacks that need disposable libp2p hosts
6. Use Antithesis randomness: `rngIntn(n)`, `rngChoice(items)`, `randomBytes(n)` — never `math/rand`
