package main

import (
	"encoding/hex"
	"log"
	"math/big"
	"sync"

	"workload/internal/foc"

	"github.com/antithesishq/antithesis-sdk-go/assert"
	"github.com/antithesishq/antithesis-sdk-go/random"
	"github.com/ipfs/go-cid"
)

// ===========================================================================
// FOC Piece Lifecycle Security
//
// Tests the full piece add/delete/retrieve lifecycle, then runs an independent
// attack probe. The first 4 phases have real ordering dependencies:
//
//   Init (upload+add) → Verified (retrieve+CID check) →
//   Deleted (schedule delete + post-delete retrieve) →
//   Checked (verify count + proving) → Attack → (back to Init)
//
// The attack phase picks one random probe per cycle:
//   - Nonce replay on addPieces
//   - Cross-dataset piece injection
//   - Double piece deletion
//   - Nonexistent piece deletion
//   - Post-termination piece addition
//
// Requires griefRuntime in griefReady state with LastOnChainDSID > 0.
// ===========================================================================

// ---------------------------------------------------------------------------
// State
// ---------------------------------------------------------------------------

type pieceSecPhase int

const (
	pieceSecInit    pieceSecPhase = iota // upload + add piece
	pieceSecVerify                       // retrieve + CID integrity check
	pieceSecDelete                       // schedule deletion + post-delete retrieval (curio#1039)
	pieceSecCheck                        // verify count decreased + proving continues
	pieceSecAttack                       // random attack probe, then reset to Init
)

func (s pieceSecPhase) String() string {
	switch s {
	case pieceSecInit:
		return "Init"
	case pieceSecVerify:
		return "Verify"
	case pieceSecDelete:
		return "Delete"
	case pieceSecCheck:
		return "Check"
	case pieceSecAttack:
		return "Attack"
	default:
		return "Unknown"
	}
}

var (
	pieceSec   pieceSecRuntime
	pieceSecMu sync.Mutex
)

type pieceSecRuntime struct {
	Phase pieceSecPhase

	// Piece under test
	PieceCID string
	PieceID  int
	Nonce    *big.Int // nonce used for addPieces (for replay test)

	// Snapshots
	CountBefore  *big.Int
	ProvenBefore uint64

	// Progress
	Cycles      int
	AttacksDone int
}

func pieceSecSnap() pieceSecRuntime {
	pieceSecMu.Lock()
	defer pieceSecMu.Unlock()
	return pieceSec
}

// ---------------------------------------------------------------------------
// DoFOCPieceSecurityProbe — deck entry
// ---------------------------------------------------------------------------

func DoFOCPieceSecurityProbe() {
	if focCfg == nil || focCfg.ClientKey == nil {
		return
	}
	if _, ok := requireReady(); !ok {
		return
	}
	gs := griefSnap()
	if gs.State != griefReady || gs.ClientKey == nil || gs.LastOnChainDSID == 0 {
		return
	}

	pieceSecMu.Lock()
	phase := pieceSec.Phase
	pieceSecMu.Unlock()

	switch phase {
	case pieceSecInit:
		pieceSecDoInit(gs)
	case pieceSecVerify:
		pieceSecDoVerify()
	case pieceSecDelete:
		pieceSecDoDelete(gs)
	case pieceSecCheck:
		pieceSecDoCheck(gs)
	case pieceSecAttack:
		pieceSecDoAttack(gs)
	}
}

// ---------------------------------------------------------------------------
// Phase 1: Upload + Add Piece
// ---------------------------------------------------------------------------

func pieceSecDoInit(gs griefRuntime) {
	if !foc.PingCurio(ctx) {
		return
	}
	node := focNode()

	// Upload a small random piece
	size := 128 + rngIntn(384)
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(random.GetRandom() & 0xFF)
	}

	pieceCID, err := foc.CalculatePieceCID(data)
	if err != nil {
		log.Printf("[foc-piece-security] CalculatePieceCID failed: %v", err)
		return
	}
	if err := foc.UploadPiece(ctx, data, pieceCID); err != nil {
		log.Printf("[foc-piece-security] UploadPiece failed: %v", err)
		return
	}
	if err := foc.WaitForPiece(ctx, pieceCID); err != nil {
		log.Printf("[foc-piece-security] WaitForPiece failed: %v", err)
		return
	}

	// Snapshot count BEFORE
	dsIDBytes := foc.EncodeBigInt(big.NewInt(int64(gs.LastOnChainDSID)))
	countBefore, _ := foc.EthCallUint256(ctx, node, focCfg.PDPAddr, foc.BuildCalldata(foc.SigGetActivePieceCount, dsIDBytes))

	// Add piece to griefing dataset
	nonce := new(big.Int).SetUint64(random.GetRandom())
	parsedCID, err := cid.Decode(pieceCID)
	if err != nil {
		return
	}

	sig, err := foc.SignEIP712AddPieces(
		gs.ClientKey, focCfg.FWSSAddr,
		gs.LastClientDSID, nonce,
		[][]byte{parsedCID.Bytes()}, nil, nil,
	)
	if err != nil {
		log.Printf("[foc-piece-security] EIP-712 signing failed: %v", err)
		return
	}

	extraData := encodeAddPiecesExtraData(nonce, 1, sig)
	txHash, err := foc.AddPiecesHTTP(ctx, gs.LastOnChainDSID, []string{pieceCID}, hex.EncodeToString(extraData))
	if err != nil {
		log.Printf("[foc-piece-security] AddPiecesHTTP failed: %v", err)
		return
	}

	pieceIDs, err := foc.WaitForPieceAddition(ctx, gs.LastOnChainDSID, txHash)
	if err != nil {
		log.Printf("[foc-piece-security] WaitForPieceAddition failed: %v", err)
		return
	}

	pieceID := 0
	if len(pieceIDs) > 0 {
		pieceID = pieceIDs[0]
	}

	// Check count increased
	countAfter, _ := foc.EthCallUint256(ctx, node, focCfg.PDPAddr, foc.BuildCalldata(foc.SigGetActivePieceCount, dsIDBytes))
	if countBefore != nil && countAfter != nil {
		assert.Sometimes(countAfter.Cmp(countBefore) > 0, "Active piece count increases after addition", map[string]any{
			"countBefore": countBefore.String(),
			"countAfter":  countAfter.String(),
		})
	}

	log.Printf("[foc-piece-security] piece added: cid=%s pieceID=%d", pieceCID, pieceID)

	pieceSecMu.Lock()
	pieceSec.PieceCID = pieceCID
	pieceSec.PieceID = pieceID
	pieceSec.Nonce = nonce
	pieceSec.CountBefore = countAfter // use post-add count as baseline for delete check
	pieceSec.Phase = pieceSecVerify
	pieceSecMu.Unlock()
}

// ---------------------------------------------------------------------------
// Phase 2: Retrieve + Verify CID Integrity
// ---------------------------------------------------------------------------

func pieceSecDoVerify() {
	s := pieceSecSnap()

	data, err := foc.DownloadPiece(ctx, s.PieceCID)
	if err != nil {
		log.Printf("[foc-piece-security] download failed for %s: %v", s.PieceCID, err)
		return
	}

	computedCID, err := foc.CalculatePieceCID(data)
	if err != nil {
		return
	}

	match := computedCID == s.PieceCID
	assert.Sometimes(match, "Retrieved piece matches uploaded CID", map[string]any{
		"pieceCID":    s.PieceCID,
		"computedCID": computedCID,
	})

	log.Printf("[foc-piece-security] retrieval verified: cid=%s match=%v", s.PieceCID, match)

	pieceSecMu.Lock()
	pieceSec.Phase = pieceSecDelete
	pieceSecMu.Unlock()
}

// ---------------------------------------------------------------------------
// Phase 3: Schedule Deletion + Post-Delete Retrieval (curio#1039)
// ---------------------------------------------------------------------------

func pieceSecDoDelete(gs griefRuntime) {
	s := pieceSecSnap()
	node := focNode()

	if s.PieceID == 0 {
		// Curio didn't return piece IDs — can't delete by ID.
		// Skip to attack phase (this IS the known gap).
		log.Printf("[foc-piece-security] pieceID=0, skipping delete (Curio didn't return IDs)")
		pieceSecMu.Lock()
		pieceSec.Phase = pieceSecAttack
		pieceSecMu.Unlock()
		return
	}

	if focCfg.SPKey == nil {
		focCfg.ReloadSPKey()
		if focCfg.SPKey == nil {
			return
		}
	}

	// Snapshot proven epoch before
	dsIDBytes := foc.EncodeBigInt(big.NewInt(int64(gs.LastOnChainDSID)))
	provenBefore, _ := foc.EthCallUint256(ctx, node, focCfg.PDPAddr, foc.BuildCalldata(foc.SigGetLastProvenEpoch, dsIDBytes))

	// Schedule deletion
	pieceIDBig := big.NewInt(int64(s.PieceID))
	sig, err := foc.SignEIP712SchedulePieceRemovals(
		gs.ClientKey, focCfg.FWSSAddr,
		gs.LastClientDSID, []*big.Int{pieceIDBig},
	)
	if err != nil {
		log.Printf("[foc-piece-security] deletion signing failed: %v", err)
		return
	}

	extraData := encodeBytes(sig)
	calldata := foc.BuildCalldata(foc.SigSchedulePieceDeletions,
		foc.EncodeBigInt(big.NewInt(int64(gs.LastOnChainDSID))),
		foc.EncodeBigInt(big.NewInt(96)),
		foc.EncodeBigInt(big.NewInt(160)),
		foc.EncodeBigInt(big.NewInt(1)),
		foc.EncodeBigInt(pieceIDBig),
		extraData,
	)

	ok := foc.SendEthTxConfirmed(ctx, node, focCfg.SPKey, focCfg.PDPAddr, calldata, "foc-piece-security-delete")
	if !ok {
		// schedulePieceDeletions through FWSS is extremely gas-heavy on FVM
		// (~29.7M gas for the cross-contract callback chain). It may hit the
		// 30M gas limit and revert. This is a known FVM cost issue, not a
		// contract logic bug. Skip to attack phase rather than retrying forever.
		log.Printf("[foc-piece-security] deletion tx failed (likely gas limit on FVM cross-contract calls), skipping to attack phase")
		assert.Sometimes(false, "Piece deletion via FWSS callback succeeds", map[string]any{
			"pieceID":   s.PieceID,
			"dataSetID": gs.LastOnChainDSID,
			"note":      "schedulePieceDeletions uses ~29.7M of 30M gas on FVM",
		})
		pieceSecMu.Lock()
		pieceSec.Phase = pieceSecAttack
		pieceSecMu.Unlock()
		return
	}

	log.Printf("[foc-piece-security] deletion succeeded: pieceID=%d", s.PieceID)
	assert.Sometimes(true, "Piece deletion via FWSS callback succeeds", map[string]any{
		"pieceID":   s.PieceID,
		"dataSetID": gs.LastOnChainDSID,
	})

	// curio#1039: immediately retrieve after deletion scheduled
	retrieveData, retrieveErr := foc.DownloadPiece(ctx, s.PieceCID)
	if retrieveErr != nil {
		log.Printf("[foc-piece-security] post-delete retrieval: %v", retrieveErr)
	} else {
		computedCID, cidErr := foc.CalculatePieceCID(retrieveData)
		if cidErr == nil {
			clean := computedCID == s.PieceCID
			assert.Sometimes(clean, "Piece retrievable after deletion scheduled", map[string]any{
				"pieceCID": s.PieceCID,
				"clean":    clean,
			})
			log.Printf("[foc-piece-security] post-delete retrieval: clean=%v", clean)
		}
	}

	var provenBeforeU64 uint64
	if provenBefore != nil {
		provenBeforeU64 = provenBefore.Uint64()
	}

	pieceSecMu.Lock()
	pieceSec.ProvenBefore = provenBeforeU64
	pieceSec.Phase = pieceSecCheck
	pieceSecMu.Unlock()
}

// ---------------------------------------------------------------------------
// Phase 4: Verify Count Decreased + Proving Continues
// ---------------------------------------------------------------------------

func pieceSecDoCheck(gs griefRuntime) {
	s := pieceSecSnap()
	node := focNode()

	dsIDBytes := foc.EncodeBigInt(big.NewInt(int64(gs.LastOnChainDSID)))

	countAfter, _ := foc.EthCallUint256(ctx, node, focCfg.PDPAddr, foc.BuildCalldata(foc.SigGetActivePieceCount, dsIDBytes))
	if s.CountBefore != nil && countAfter != nil {
		decreased := countAfter.Cmp(s.CountBefore) < 0
		assert.Sometimes(decreased, "Active piece count decreases after deletion", map[string]any{
			"countBefore": s.CountBefore.String(),
			"countAfter":  countAfter.String(),
		})
	}

	provenAfter, _ := foc.EthCallUint256(ctx, node, focCfg.PDPAddr, foc.BuildCalldata(foc.SigGetLastProvenEpoch, dsIDBytes))
	if provenAfter != nil {
		assert.Sometimes(provenAfter.Uint64() >= s.ProvenBefore, "Proving continues after piece deletion", map[string]any{
			"provenBefore": s.ProvenBefore,
			"provenAfter":  provenAfter.Uint64(),
		})
	}

	pieceSecMu.Lock()
	pieceSec.Phase = pieceSecAttack
	pieceSecMu.Unlock()
}

// ---------------------------------------------------------------------------
// Phase 5: Random Attack Probe, then reset
// ---------------------------------------------------------------------------

func pieceSecDoAttack(gs griefRuntime) {
	s := pieceSecSnap()

	type attack struct {
		name string
		fn   func(griefRuntime, pieceSecRuntime)
	}
	attacks := []attack{
		{"NonceReplay", attackNonceReplay},
		{"CrossDatasetInject", attackCrossDataset},
		{"DoubleDeletion", attackDoubleDeletion},
		{"NonexistentDelete", attackNonexistentDelete},
		{"PostTerminationAdd", attackPostTerminationAdd},
	}

	pick := attacks[rngIntn(len(attacks))]
	log.Printf("[foc-piece-security] attack: %s", pick.name)
	pick.fn(gs, s)

	// Cycle complete — reset
	pieceSecMu.Lock()
	pieceSec.AttacksDone++
	pieceSec.Cycles++
	cycles := pieceSec.Cycles
	pieceSec.Phase = pieceSecInit
	pieceSec.PieceCID = ""
	pieceSec.PieceID = 0
	pieceSec.Nonce = nil
	pieceSec.CountBefore = nil
	pieceSec.ProvenBefore = 0
	pieceSecMu.Unlock()

	log.Printf("[foc-piece-security] cycle %d complete", cycles)
	assert.Sometimes(true, "Piece security cycle completes", map[string]any{"cycles": cycles})
}

// ---------------------------------------------------------------------------
// Attack: Nonce Replay on addPieces
// ---------------------------------------------------------------------------

func attackNonceReplay(gs griefRuntime, s pieceSecRuntime) {
	if s.Nonce == nil || !foc.PingCurio(ctx) {
		return
	}

	data := make([]byte, 128)
	for i := range data {
		data[i] = byte(random.GetRandom() & 0xFF)
	}
	newCID, err := foc.CalculatePieceCID(data)
	if err != nil {
		return
	}
	_ = foc.UploadPiece(ctx, data, newCID)
	_ = foc.WaitForPiece(ctx, newCID)

	parsedCID, err := cid.Decode(newCID)
	if err != nil {
		return
	}

	// Sign with the SAME nonce as the previous add
	sig, err := foc.SignEIP712AddPieces(
		gs.ClientKey, focCfg.FWSSAddr,
		gs.LastClientDSID, s.Nonce,
		[][]byte{parsedCID.Bytes()}, nil, nil,
	)
	if err != nil {
		return
	}

	extraData := encodeAddPiecesExtraData(s.Nonce, 1, sig)
	_, httpErr := foc.AddPiecesHTTP(ctx, gs.LastOnChainDSID, []string{newCID}, hex.EncodeToString(extraData))

	if httpErr != nil {
		assert.Sometimes(true, "AddPieces nonce replay rejected", map[string]any{"nonce": s.Nonce.String()})
	} else {
		log.Printf("[foc-piece-security] CRITICAL: nonce replay accepted by Curio HTTP")
		assert.Sometimes(false, "AddPieces nonce replay rejected", map[string]any{"nonce": s.Nonce.String()})
	}
}

// ---------------------------------------------------------------------------
// Attack: Cross-Dataset Piece Injection
// ---------------------------------------------------------------------------

func attackCrossDataset(gs griefRuntime, _ pieceSecRuntime) {
	if !foc.PingCurio(ctx) {
		return
	}
	focS := snap()
	if focS.OnChainDataSetID == 0 || gs.LastOnChainDSID == 0 || focS.OnChainDataSetID == gs.LastOnChainDSID {
		return
	}

	data := make([]byte, 128)
	for i := range data {
		data[i] = byte(random.GetRandom() & 0xFF)
	}
	newCID, err := foc.CalculatePieceCID(data)
	if err != nil {
		return
	}
	_ = foc.UploadPiece(ctx, data, newCID)
	_ = foc.WaitForPiece(ctx, newCID)

	parsedCID, err := cid.Decode(newCID)
	if err != nil {
		return
	}
	nonce := new(big.Int).SetUint64(random.GetRandom())

	// Sign for GRIEFING dataset, submit to PRIMARY dataset
	sig, err := foc.SignEIP712AddPieces(
		gs.ClientKey, focCfg.FWSSAddr,
		gs.LastClientDSID, nonce,
		[][]byte{parsedCID.Bytes()}, nil, nil,
	)
	if err != nil {
		return
	}

	extraData := encodeAddPiecesExtraData(nonce, 1, sig)
	_, httpErr := foc.AddPiecesHTTP(ctx, focS.OnChainDataSetID, []string{newCID}, hex.EncodeToString(extraData))

	if httpErr != nil {
		assert.Sometimes(true, "Cross-dataset piece injection rejected", map[string]any{
			"signedFor": gs.LastOnChainDSID, "submittedTo": focS.OnChainDataSetID,
		})
	} else {
		log.Printf("[foc-piece-security] CRITICAL: cross-dataset injection accepted")
		assert.Sometimes(false, "Cross-dataset piece injection rejected", map[string]any{
			"signedFor": gs.LastOnChainDSID, "submittedTo": focS.OnChainDataSetID,
		})
	}
}

// ---------------------------------------------------------------------------
// Attack: Double Piece Deletion
// ---------------------------------------------------------------------------

func attackDoubleDeletion(gs griefRuntime, s pieceSecRuntime) {
	if s.PieceID == 0 || focCfg.SPKey == nil {
		return
	}
	node := focNode()

	pieceIDBig := big.NewInt(int64(s.PieceID))
	sig, err := foc.SignEIP712SchedulePieceRemovals(
		gs.ClientKey, focCfg.FWSSAddr,
		gs.LastClientDSID, []*big.Int{pieceIDBig},
	)
	if err != nil {
		return
	}

	extraData := encodeBytes(sig)
	calldata := foc.BuildCalldata(foc.SigSchedulePieceDeletions,
		foc.EncodeBigInt(big.NewInt(int64(gs.LastOnChainDSID))),
		foc.EncodeBigInt(big.NewInt(96)),
		foc.EncodeBigInt(big.NewInt(160)),
		foc.EncodeBigInt(big.NewInt(1)),
		foc.EncodeBigInt(pieceIDBig),
		extraData,
	)

	ok := foc.SendEthTxConfirmed(ctx, node, focCfg.SPKey, focCfg.PDPAddr, calldata, "foc-piece-security-double-del")

	if !ok {
		assert.Sometimes(true, "Double piece deletion rejected", map[string]any{"pieceID": s.PieceID})
	} else {
		log.Printf("[foc-piece-security] CRITICAL: double deletion succeeded for pieceID=%d", s.PieceID)
		assert.Sometimes(false, "Double piece deletion rejected", map[string]any{"pieceID": s.PieceID})
	}
}

// ---------------------------------------------------------------------------
// Attack: Nonexistent Piece Deletion
// ---------------------------------------------------------------------------

func attackNonexistentDelete(gs griefRuntime, _ pieceSecRuntime) {
	if focCfg.SPKey == nil {
		return
	}
	node := focNode()

	fakePieceID := big.NewInt(int64(999999 + rngIntn(1000000)))
	sig, err := foc.SignEIP712SchedulePieceRemovals(
		gs.ClientKey, focCfg.FWSSAddr,
		gs.LastClientDSID, []*big.Int{fakePieceID},
	)
	if err != nil {
		return
	}

	extraData := encodeBytes(sig)
	calldata := foc.BuildCalldata(foc.SigSchedulePieceDeletions,
		foc.EncodeBigInt(big.NewInt(int64(gs.LastOnChainDSID))),
		foc.EncodeBigInt(big.NewInt(96)),
		foc.EncodeBigInt(big.NewInt(160)),
		foc.EncodeBigInt(big.NewInt(1)),
		foc.EncodeBigInt(fakePieceID),
		extraData,
	)

	ok := foc.SendEthTxConfirmed(ctx, node, focCfg.SPKey, focCfg.PDPAddr, calldata, "foc-piece-security-fake-del")

	if !ok {
		assert.Sometimes(true, "Nonexistent piece deletion rejected", map[string]any{"fakeID": fakePieceID.String()})
	} else {
		log.Printf("[foc-piece-security] CRITICAL: nonexistent piece deletion succeeded (fakeID=%s)", fakePieceID)
		assert.Sometimes(false, "Nonexistent piece deletion rejected", map[string]any{"fakeID": fakePieceID.String()})
	}
}

// ---------------------------------------------------------------------------
// Attack: Post-Termination Piece Addition
// ---------------------------------------------------------------------------

func attackPostTerminationAdd(gs griefRuntime, _ pieceSecRuntime) {
	if focCfg.SPKey == nil || !foc.PingCurio(ctx) {
		return
	}
	node := focNode()

	// Terminate the griefing dataset's service
	calldata := foc.BuildCalldata(foc.SigTerminateService,
		foc.EncodeBigInt(gs.LastClientDSID),
	)
	ok := foc.SendEthTxConfirmed(ctx, node, focCfg.SPKey, focCfg.FWSSAddr, calldata, "foc-piece-security-terminate")
	if !ok {
		log.Printf("[foc-piece-security] terminateService failed")
		return
	}

	// Immediately try to add a piece — should be rejected
	data := make([]byte, 128)
	for i := range data {
		data[i] = byte(random.GetRandom() & 0xFF)
	}
	newCID, err := foc.CalculatePieceCID(data)
	if err != nil {
		return
	}
	_ = foc.UploadPiece(ctx, data, newCID)
	_ = foc.WaitForPiece(ctx, newCID)

	parsedCID, err := cid.Decode(newCID)
	if err != nil {
		return
	}
	nonce := new(big.Int).SetUint64(random.GetRandom())
	sig, err := foc.SignEIP712AddPieces(
		gs.ClientKey, focCfg.FWSSAddr,
		gs.LastClientDSID, nonce,
		[][]byte{parsedCID.Bytes()}, nil, nil,
	)
	if err != nil {
		return
	}

	extraData := encodeAddPiecesExtraData(nonce, 1, sig)
	_, httpErr := foc.AddPiecesHTTP(ctx, gs.LastOnChainDSID, []string{newCID}, hex.EncodeToString(extraData))

	if httpErr != nil {
		assert.Sometimes(true, "Piece addition blocked after termination", map[string]any{"dsID": gs.LastOnChainDSID})
		log.Printf("[foc-piece-security] post-termination add correctly rejected")
	} else {
		log.Printf("[foc-piece-security] CRITICAL: post-termination add ACCEPTED for dataset=%d", gs.LastOnChainDSID)
		assert.Sometimes(false, "Piece addition blocked after termination", map[string]any{"dsID": gs.LastOnChainDSID})
	}
}

// ---------------------------------------------------------------------------
// Progress
// ---------------------------------------------------------------------------

func logPieceSecProgress() {
	s := pieceSecSnap()
	if s.Cycles > 0 || s.Phase != pieceSecInit {
		log.Printf("[foc-piece-security] phase=%s cycles=%d attacks=%d", s.Phase, s.Cycles, s.AttacksDone)
	}
}
