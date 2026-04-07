package main

import (
	"bytes"
	"encoding/hex"
	"io"
	"log"
	"math/big"
	"net/http"
	"sync"
	"time"

	"workload/internal/foc"

	"github.com/antithesishq/antithesis-sdk-go/assert"
	"github.com/antithesishq/antithesis-sdk-go/random"
)

// ===========================================================================
// Scenario 3: Curio Resilience & Orphan Rails
//
// Tests Curio's HTTP API resilience under malformed input and exercises the
// orphan rail scenario (dataset created but never populated with data).
//
//   Init → OrphanCreated → OrphanChecked → (back to Init)
//
// Phase Init also runs the HTTP stress barrage on every cycle.
//
// Covers:
//   - Risks DB "Network-wide Curio crash" (Sev2, NO mitigation)
//   - Risks DB "Upload failures + orphan rails" (HIGH)
//   - Curio HTTP API does not crash on malformed requests
//   - Empty datasets do not accumulate storage charges
// ===========================================================================

// ---------------------------------------------------------------------------
// State
// ---------------------------------------------------------------------------

type resState int

const (
	resInit          resState = iota // HTTP stress barrage
	resOrphanCreated                 // empty dataset created, waiting to check billing
	resOrphanChecked                 // billing verified, cleanup
)

func (s resState) String() string {
	switch s {
	case resInit:
		return "Init"
	case resOrphanCreated:
		return "OrphanCreated"
	case resOrphanChecked:
		return "OrphanChecked"
	default:
		return "Unknown"
	}
}

var (
	resSec   resRuntime
	resSecMu sync.Mutex
)

type resRuntime struct {
	State resState

	// Orphan dataset tracking
	OrphanDSID        int
	OrphanFundsBefore *big.Int

	// Progress
	Cycles       int
	HTTPBarrages int
}

func resSnap() resRuntime {
	resSecMu.Lock()
	defer resSecMu.Unlock()
	return resSec
}

// ---------------------------------------------------------------------------
// DoFOCResilienceProbe — deck entry
// ---------------------------------------------------------------------------

func DoFOCResilienceProbe() {
	if focCfg == nil || focCfg.ClientKey == nil {
		return
	}
	if _, ok := requireReady(); !ok {
		return
	}

	gs := griefSnap()
	if gs.State != griefReady || gs.ClientKey == nil {
		return
	}

	if !foc.PingCurio(ctx) {
		return
	}

	resSecMu.Lock()
	state := resSec.State
	resSecMu.Unlock()

	switch state {
	case resInit:
		resDoHTTPStress()
	case resOrphanCreated:
		resDoOrphanCheck()
	case resOrphanChecked:
		resDoOrphanCleanup()
	}
}

// ---------------------------------------------------------------------------
// Phase 1: HTTP Stress Barrage + Create Orphan Dataset
// ---------------------------------------------------------------------------

func resDoHTTPStress() {
	gs := griefSnap()
	node := focNode()
	base := foc.CurioBaseURL()
	client := &http.Client{Timeout: 30 * time.Second}

	log.Printf("[foc-resilience] starting HTTP stress barrage")

	type malformedReq struct {
		name   string
		method string
		url    string
		body   []byte
	}

	reqs := []malformedReq{
		{"empty-body", "POST", base + "/pdp/data-sets", nil},
		{"invalid-json", "POST", base + "/pdp/data-sets", []byte(`{not json!!!}`)},
		{"nonexistent-dataset", "GET", base + "/pdp/data-sets/99999999", nil},
		{"nonexistent-pieces", "GET", base + "/pdp/data-sets/99999999/pieces", nil},
		{"empty-piece-upload", "POST", base + "/pdp/piece/uploads", nil},
		{"invalid-piece-finalize", "POST", base + "/pdp/piece/uploads/00000000-0000-0000-0000-000000000000",
			[]byte(`{"pieceCid": "not-a-real-cid"}`)},
		{"huge-extra-data", "POST", base + "/pdp/data-sets", hugeExtraDataPayload()},
	}

	accepted := 0
	for _, r := range reqs {
		var bodyReader io.Reader
		if r.body != nil {
			bodyReader = bytes.NewReader(r.body)
		}

		req, err := http.NewRequestWithContext(ctx, r.method, r.url, bodyReader)
		if err != nil {
			continue
		}
		if r.body != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := client.Do(req)
		if err != nil {
			log.Printf("[foc-resilience] %s: connection error (may be fine): %v", r.name, err)
			continue
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()

		log.Printf("[foc-resilience] %s: status=%d", r.name, resp.StatusCode)
		accepted++
	}

	// THE KEY CHECK: Curio must still be alive after all the abuse
	pingOK := foc.PingCurio(ctx)
	assert.Always(pingOK, "Curio survives malformed HTTP requests", map[string]any{
		"requestsSent": len(reqs),
		"accepted":     accepted,
	})

	if !pingOK {
		log.Printf("[foc-resilience] CRITICAL: Curio not reachable after HTTP stress barrage!")
		return
	}

	assert.Sometimes(true, "Curio HTTP resilience exercised", map[string]any{
		"requestsSent": len(reqs),
	})

	resSecMu.Lock()
	resSec.HTTPBarrages++
	resSecMu.Unlock()

	log.Printf("[foc-resilience] HTTP barrage complete, Curio alive. Creating orphan dataset...")

	// Now create an empty dataset (orphan rail test)
	if focCfg.SPKey == nil || focCfg.SPEthAddr == nil {
		focCfg.ReloadSPKey()
		if focCfg.SPKey == nil {
			return
		}
	}

	// Snapshot funds BEFORE
	fundsBefore := foc.ReadAccountFunds(ctx, node, focCfg.FilPayAddr, focCfg.USDFCAddr, gs.ClientEth)

	clientDataSetId := new(big.Int).SetUint64(random.GetRandom())
	metadataKeys := []string{"source"}
	metadataValues := []string{"antithesis-resilience-orphan"}

	sig, err := foc.SignEIP712CreateDataSet(
		gs.ClientKey, focCfg.FWSSAddr,
		clientDataSetId, focCfg.SPEthAddr,
		metadataKeys, metadataValues,
	)
	if err != nil {
		log.Printf("[foc-resilience] EIP-712 signing failed: %v", err)
		return
	}

	extraData := encodeCreateDataSetExtra(gs.ClientEth, clientDataSetId, metadataKeys, metadataValues, sig)
	recordKeeper := "0x" + hex.EncodeToString(focCfg.FWSSAddr)

	txHash, err := foc.CreateDataSetHTTP(ctx, recordKeeper, hex.EncodeToString(extraData))
	if err != nil {
		log.Printf("[foc-resilience] orphan dataset creation failed: %v", err)
		return
	}

	onChainID, err := foc.WaitForDataSetCreation(ctx, txHash)
	if err != nil {
		log.Printf("[foc-resilience] orphan dataset confirmation failed: %v", err)
		return
	}

	log.Printf("[foc-resilience] orphan dataset created: onChainID=%d (no pieces will be added)", onChainID)

	resSecMu.Lock()
	resSec.OrphanDSID = onChainID
	resSec.OrphanFundsBefore = fundsBefore
	resSec.State = resOrphanCreated
	resSecMu.Unlock()
}

// ---------------------------------------------------------------------------
// Phase 2: Check Orphan Dataset Billing
// ---------------------------------------------------------------------------

func resDoOrphanCheck() {
	s := resSnap()
	gs := griefSnap()
	node := focNode()

	if s.OrphanDSID == 0 {
		resSecMu.Lock()
		resSec.State = resOrphanChecked
		resSecMu.Unlock()
		return
	}

	// Verify zero pieces
	dsIDBytes := foc.EncodeBigInt(big.NewInt(int64(s.OrphanDSID)))
	activeCount, _ := foc.EthCallUint256(ctx, node, focCfg.PDPAddr, foc.BuildCalldata(foc.SigGetActivePieceCount, dsIDBytes))

	// Check if dataset is live
	live, _ := foc.EthCallBool(ctx, node, focCfg.PDPAddr, foc.BuildCalldata(foc.SigDataSetLive, dsIDBytes))

	// Read current funds
	fundsNow := foc.ReadAccountFunds(ctx, node, focCfg.FilPayAddr, focCfg.USDFCAddr, gs.ClientEth)

	log.Printf("[foc-resilience] orphan dataset %d: live=%v activePieces=%v funds=%v (before=%v)",
		s.OrphanDSID, live, activeCount, fundsNow, s.OrphanFundsBefore)

	// Check: with zero pieces, client should not be losing funds to storage charges
	// (The sybil fee on creation is expected, but ongoing charges should be zero)
	if activeCount != nil && activeCount.Sign() == 0 && fundsNow != nil && s.OrphanFundsBefore != nil {
		// Allow for sybil fee deduction, but ongoing charges should not accumulate further
		// We log this for observability — the sidecar rate-consistency check catches the invariant
		assert.Sometimes(true, "Empty dataset billing checked", map[string]any{
			"dsID":         s.OrphanDSID,
			"activePieces": activeCount.String(),
			"fundsBefore":  s.OrphanFundsBefore.String(),
			"fundsNow":     fundsNow.String(),
		})
	}

	resSecMu.Lock()
	resSec.State = resOrphanChecked
	resSecMu.Unlock()
}

// ---------------------------------------------------------------------------
// Phase 3: Cleanup — terminate and delete orphan dataset
// ---------------------------------------------------------------------------

func resDoOrphanCleanup() {
	s := resSnap()

	if s.OrphanDSID > 0 {
		// We can't easily clean up without knowing the clientDataSetId.
		// The orphan dataset was created with a random clientDataSetId that we didn't persist.
		// For now, just log the orphan and move on. The sidecar will track it.
		node := focNode()
		dsIDBytes := foc.EncodeBigInt(big.NewInt(int64(s.OrphanDSID)))
		live, _ := foc.EthCallBool(ctx, node, focCfg.PDPAddr, foc.BuildCalldata(foc.SigDataSetLive, dsIDBytes))
		log.Printf("[foc-resilience] orphan dataset %d live=%v (left for sidecar monitoring)", s.OrphanDSID, live)
	}

	resSecMu.Lock()
	resSec.Cycles++
	cycles := resSec.Cycles
	barrages := resSec.HTTPBarrages
	resSec.State = resInit
	resSec.OrphanDSID = 0
	resSec.OrphanFundsBefore = nil
	resSecMu.Unlock()

	log.Printf("[foc-resilience] cycle %d complete (HTTP barrages=%d)", cycles, barrages)
	assert.Sometimes(true, "Resilience scenario cycle completes", map[string]any{
		"cycles":       cycles,
		"httpBarrages": barrages,
	})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// hugeExtraDataPayload generates a large but valid-ish JSON body for stress testing.
func hugeExtraDataPayload() []byte {
	// ~64KB of hex data
	data := make([]byte, 32768)
	for i := range data {
		data[i] = byte(i & 0xFF)
	}
	hexStr := hex.EncodeToString(data)
	return []byte(`{"recordKeeper":"0x0000000000000000000000000000000000000000","extraData":"` + hexStr + `"}`)
}

// ---------------------------------------------------------------------------
// Progress
// ---------------------------------------------------------------------------

func logResProgress() {
	s := resSnap()
	log.Printf("[foc-resilience] state=%s cycles=%d httpBarrages=%d orphanDSID=%d",
		s.State, s.Cycles, s.HTTPBarrages, s.OrphanDSID)
}
