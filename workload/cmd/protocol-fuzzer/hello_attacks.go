package main

import (
	"context"
	"io"
	"log"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multihash"
)

const helloProtocol = "/fil/hello/1.0.0"

// buildHelloMessage builds a Hello protocol message as CBOR.
// HelloMessage wire format: [HeaviestTipSet []CID, HeaviestTipSetHeight int64, HeaviestTipSetWeight BigInt-bytes, GenesisHash CID]
func buildHelloMessage(tipset []cid.Cid, height uint64, weight uint64, genesis cid.Cid) []byte {
	return cborArray(
		cborCIDArray(tipset),
		cborUint64(height),
		cborBytes(bigIntBytes(weight)),
		cborCID(genesis),
	)
}

// parseGenesisCID converts the genesis CID string discovered at startup to a cid.Cid.
func parseGenesisCID() cid.Cid {
	if genesisCID == "" {
		return randomCID()
	}
	c, err := cid.Decode(genesisCID)
	if err != nil {
		log.Printf("[protocol-fuzzer] cannot parse genesis CID %q: %v, using random", genesisCID, err)
		return randomCID()
	}
	return c
}

// openHelloStream connects to the target and opens a Hello protocol stream.
func openHelloStream(ctx context.Context, h host.Host, target peer.AddrInfo) (*wrappedStream, error) {
	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := h.Connect(connectCtx, target); err != nil {
		return nil, err
	}

	streamCtx, streamCancel := context.WithTimeout(ctx, 10*time.Second)
	defer streamCancel()

	s, err := h.NewStream(streamCtx, target.ID, helloProtocol)
	if err != nil {
		return nil, err
	}
	return &wrappedStream{s}, nil
}

// wrappedStream wraps network.Stream to provide convenience methods.
type wrappedStream struct {
	s interface {
		Write([]byte) (int, error)
		Read([]byte) (int, error)
		Close() error
		CloseWrite() error
		Reset() error
	}
}

func (w *wrappedStream) Write(b []byte) (int, error) { return w.s.Write(b) }
func (w *wrappedStream) Close() error                { return w.s.Close() }
func (w *wrappedStream) CloseWrite() error           { return w.s.CloseWrite() }
func (w *wrappedStream) Reset() error                { return w.s.Reset() }

// Ensure the multihash import is used (needed for randomCID in cbor_helpers.go).
var _ = multihash.SHA2_256

// ---------------------------------------------------------------------------
// Hello Protocol Attack Vectors
//
// The Lotus Hello handler (node/hello/hello.go:HandleStream) has NO recover().
// A panic in ReadCborRPC or downstream processing (FetchTipSet, NewTipSet)
// crashes the entire node process.
// ---------------------------------------------------------------------------

func getAllHelloAttacks() []namedAttack {
	return []namedAttack{
		{name: "hello/malformed-cbor", fn: helloMalformedCBOR},
		{name: "hello/nil-tipset-cids", fn: helloNilTipsetCIDs},
		{name: "hello/huge-tipset-array", fn: helloHugeTipsetArray},
		{name: "hello/overflow-weight", fn: helloOverflowWeight},
	}
}

// sendHelloAttackPayload connects to a target and sends raw bytes on the
// Hello protocol stream, then drains the response.
func sendHelloAttackPayload(target TargetNode, payload []byte) {
	h, err := pool.GetFresh(ctx)
	if err != nil {
		debugLog("[hello] create host failed: %v", err)
		return
	}
	defer h.Close()

	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := h.Connect(connectCtx, target.AddrInfo); err != nil {
		debugLog("[hello] connect to %s failed: %v", target.Name, err)
		return
	}

	streamCtx, streamCancel := context.WithTimeout(ctx, 10*time.Second)
	defer streamCancel()
	s, err := h.NewStream(streamCtx, target.AddrInfo.ID, helloProtocol)
	if err != nil {
		debugLog("[hello] stream to %s failed: %v", target.Name, err)
		return
	}
	defer s.Close()

	s.Write(payload)
	s.CloseWrite()
	io.Copy(io.Discard, io.LimitReader(s, 1024))
}

// helloMalformedCBOR sends garbage to the Hello handler.
func helloMalformedCBOR() {
	target := rngChoice(targets)
	var payload []byte
	switch rngIntn(4) {
	case 0: // truncated valid message
		full := buildHelloMessage([]cid.Cid{randomCID()}, 1, 1, randomCID())
		payload = full[:1+rngIntn(len(full))]
	case 1: // wrong field count (3 instead of 4)
		payload = cborArray(cborCIDArray([]cid.Cid{randomCID()}), cborUint64(1), cborBytes(bigIntBytes(1)))
	case 2: // wrong types
		payload = cborArray(cborUint64(42), cborBytes([]byte{0xff}), cborNil(), cborUint64(0))
	case 3: // pure garbage
		payload = randomBytes(32 + rngIntn(256))
	}
	sendHelloAttackPayload(target, payload)
}

// helloNilTipsetCIDs sends Hello with empty CID array.
// FetchTipSet is called with these CIDs — empty TipSetKey may panic.
func helloNilTipsetCIDs() {
	target := rngChoice(targets)
	genesis := parseGenesisCID()
	payload := buildHelloMessage([]cid.Cid{}, 1, 999999999, genesis)
	sendHelloAttackPayload(target, payload)
}

// helloHugeTipsetArray sends Hello claiming massive tipset.
// Lotus creates TipSetKey from all CIDs — large array causes allocation.
func helloHugeTipsetArray() {
	target := rngChoice(targets)
	genesis := parseGenesisCID()
	numCIDs := 1000 + rngIntn(4000)
	cids := make([]cid.Cid, numCIDs)
	for i := range cids {
		cids[i] = randomCID()
	}
	payload := buildHelloMessage(cids, 1, 999999999, genesis)
	sendHelloAttackPayload(target, payload)
}

// helloOverflowWeight sends Hello with edge-case BigInt weight values.
// Tests BigInt deserialization edge cases that may cause panics.
func helloOverflowWeight() {
	target := rngChoice(targets)
	genesis := parseGenesisCID()

	var weightBytes []byte
	switch rngIntn(4) {
	case 0: // negative BigInt
		weightBytes = []byte{0x01, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}
	case 1: // empty (zero)
		weightBytes = nil
	case 2: // huge (256-bit)
		weightBytes = append([]byte{0x00}, randomBytes(32)...)
	case 3: // sign byte only
		weightBytes = []byte{0x01}
	}

	payload := cborArray(
		cborCIDArray([]cid.Cid{randomCID()}),
		cborUint64(1),
		cborBytes(weightBytes),
		cborCID(genesis),
	)
	sendHelloAttackPayload(target, payload)
}
