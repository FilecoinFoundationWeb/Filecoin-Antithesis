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
// Scenario 1: Piece Lifecycle Security
//
// Tests the full piece add/delete/retrieve lifecycle with security edge cases.
// Each deck invocation advances one phase. The scenario cycles continuously:
//
//   Init → Added → Verified → DeleteScheduled → DeleteVerified →
//   AttackPhase → Terminated → Cleanup → (back to Init)
//
// Covers:
//   - Piece add/delete accounting correctness
//   - Retrieval integrity before and after deletion (curio#1039)
//   - Proving continuity after deletion
//   - Nonce replay attacks on addPieces (EIP-712)
//   - Cross-dataset piece injection
//   - Double piece deletion
//   - Post-termination piece addition race
//
// Requires griefRuntime to be in griefReady state (secondary client set up).
// ===========================================================================

// ---------------------------------------------------------------------------
// State
// ---------------------------------------------------------------------------

type pieceSecState int

const (
	pieceSecInit            pieceSecState = iota // upload piece to Curio
	pieceSecAdded                                // piece added on-chain, snapshot counts
	pieceSecVerified                             // retrieval integrity verified
	pieceSecDeleteScheduled                      // deletion scheduled, re-retrieve check
	pieceSecDeleteVerified                       // piece count decreased, proving OK
	pieceSecAttackPhase                          // random attack probe
	pieceSecTerminated                           // terminate service, try post-term add
	pieceSecCleanup                              // delete dataset, re-create, reset
)

func (s pieceSecState) String() string {
	switch s {
	case pieceSecInit:
		return "Init"
	case pieceSecAdded:
		return "Added"
	case pieceSecVerified:
		return "Verified"
	case pieceSecDeleteScheduled:
		return "DeleteScheduled"
	case pieceSecDeleteVerified:
		return "DeleteVerified"
	case pieceSecAttackPhase:
		return "AttackPhase"
	case pieceSecTerminated:
		return "Terminated"
	case pieceSecCleanup:
		return "Cleanup"
	default:
		return "Unknown"
	}
}

var (
	pieceSec   pieceSecRuntime
	pieceSecMu sync.Mutex
)

type pieceSecRuntime struct {
	State pieceSecState

	// Piece under test
	PieceCID string
	PieceID  int
	Nonce    *big.Int // nonce used for the addPieces EIP-712 signature

	// Snapshots for before/after comparison
	CountBefore    *big.Int
	CountAfter     *big.Int
	ProvenBefore   uint64
	TermDataSetID  int      // dataset ID being terminated (for cleanup)
	TermClientDSID *big.Int // clientDataSetId for the terminated dataset

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

	// Wait for griefing secondary client to be ready
	gs := griefSnap()
	if gs.State != griefReady || gs.ClientKey == nil {
		return
	}
	if gs.LastOnChainDSID == 0 {
		return // need a griefing dataset to operate on
	}

	pieceSecMu.Lock()
	state := pieceSec.State
	pieceSecMu.Unlock()

	switch state {
	case pieceSecInit:
		pieceSecDoInit()
	case pieceSecAdded:
		pieceSecDoVerify()
	case pieceSecVerified:
		pieceSecDoScheduleDelete()
	case pieceSecDeleteScheduled:
		pieceSecDoVerifyDelete()
	case pieceSecDeleteVerified:
		pieceSecDoAttack()
	case pieceSecAttackPhase:
		pieceSecDoTerminate()
	case pieceSecTerminated:
		pieceSecDoCleanup()
	case pieceSecCleanup:
		// Cleanup resets to Init internally
		pieceSecDoCleanup()
	}
}

// ---------------------------------------------------------------------------
// Phase 1: Upload + Add Piece
// ---------------------------------------------------------------------------

func pieceSecDoInit() {
	if !foc.PingCurio(ctx) {
		return
	}
	gs := griefSnap()
	node := focNode()

	// Upload a small random piece
	size := 128 + rngIntn(384)
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(random.GetRandom() & 0xFF)
	}

	pieceCID, err := foc.CalculatePieceCID(data)
	if err != nil {
		log.Printf("[piece-security] CalculatePieceCID failed: %v", err)
		return
	}

	if err := foc.UploadPiece(ctx, data, pieceCID); err != nil {
		log.Printf("[piece-security] UploadPiece failed: %v", err)
		return
	}

	if err := foc.WaitForPiece(ctx, pieceCID); err != nil {
		log.Printf("[piece-security] WaitForPiece failed: %v", err)
		return
	}

	// Snapshot active piece count BEFORE
	dsIDBytes := foc.EncodeBigInt(big.NewInt(int64(gs.LastOnChainDSID)))
	countBefore, _ := foc.EthCallUint256(ctx, node, focCfg.PDPAddr, foc.BuildCalldata(foc.SigGetActivePieceCount, dsIDBytes))

	// Add piece to griefing dataset
	nonce := new(big.Int).SetUint64(random.GetRandom())

	parsedCID, err := cid.Decode(pieceCID)
	if err != nil {
		log.Printf("[piece-security] CID decode failed: %v", err)
		return
	}
	cidBytes := parsedCID.Bytes()

	sig, err := foc.SignEIP712AddPieces(
		gs.ClientKey, focCfg.FWSSAddr,
		gs.LastClientDSID, nonce,
		[][]byte{cidBytes}, nil, nil,
	)
	if err != nil {
		log.Printf("[piece-security] EIP-712 signing failed: %v", err)
		return
	}

	extraData := encodeAddPiecesExtraData(nonce, 1, sig)
	txHash, err := foc.AddPiecesHTTP(ctx, gs.LastOnChainDSID, []string{pieceCID}, hex.EncodeToString(extraData))
	if err != nil {
		log.Printf("[piece-security] AddPiecesHTTP failed: %v", err)
		return
	}

	pieceIDs, err := foc.WaitForPieceAddition(ctx, gs.LastOnChainDSID, txHash)
	if err != nil {
		log.Printf("[piece-security] WaitForPieceAddition failed: %v", err)
		return
	}

	pieceID := 0
	if len(pieceIDs) > 0 {
		pieceID = pieceIDs[0]
	}

	// Snapshot count AFTER
	countAfter, _ := foc.EthCallUint256(ctx, node, focCfg.PDPAddr, foc.BuildCalldata(foc.SigGetActivePieceCount, dsIDBytes))

	if countBefore != nil && countAfter != nil {
		increased := countAfter.Cmp(countBefore) > 0
		assert.Sometimes(increased, "Active piece count increases after addition", map[string]any{
			"countBefore": countBefore.String(),
			"countAfter":  countAfter.String(),
			"pieceCID":    pieceCID,
		})
	}

	log.Printf("[piece-security] piece added: cid=%s pieceID=%d countBefore=%v countAfter=%v",
		pieceCID, pieceID, countBefore, countAfter)

	pieceSecMu.Lock()
	pieceSec.PieceCID = pieceCID
	pieceSec.PieceID = pieceID
	pieceSec.Nonce = nonce
	pieceSec.CountBefore = countBefore
	pieceSec.CountAfter = countAfter
	pieceSec.State = pieceSecAdded
	pieceSecMu.Unlock()
}

// ---------------------------------------------------------------------------
// Phase 2: Retrieve + Verify CID Integrity
// ---------------------------------------------------------------------------

func pieceSecDoVerify() {
	s := pieceSecSnap()

	data, err := foc.DownloadPiece(ctx, s.PieceCID)
	if err != nil {
		log.Printf("[piece-security] download failed for %s: %v", s.PieceCID, err)
		return // will retry next invocation
	}

	computedCID, err := foc.CalculatePieceCID(data)
	if err != nil {
		log.Printf("[piece-security] CalculatePieceCID failed: %v", err)
		return
	}

	match := computedCID == s.PieceCID
	assert.Sometimes(match, "Retrieved piece matches uploaded CID", map[string]any{
		"pieceCID":    s.PieceCID,
		"computedCID": computedCID,
		"dataLen":     len(data),
	})

	if !match {
		log.Printf("[piece-security] INTEGRITY MISMATCH: expected=%s computed=%s", s.PieceCID, computedCID)
	} else {
		log.Printf("[piece-security] integrity verified: cid=%s", s.PieceCID)
	}

	pieceSecMu.Lock()
	pieceSec.State = pieceSecVerified
	pieceSecMu.Unlock()
}

// ---------------------------------------------------------------------------
// Phase 3: Schedule Deletion + Re-Retrieve (curio#1039)
// ---------------------------------------------------------------------------

func pieceSecDoScheduleDelete() {
	s := pieceSecSnap()
	gs := griefSnap()
	node := focNode()

	if s.PieceID == 0 {
		// Can't delete without a valid piece ID — skip to attack phase
		log.Printf("[piece-security] skipping delete (pieceID=0), advancing to attack phase")
		pieceSecMu.Lock()
		pieceSec.State = pieceSecDeleteVerified
		pieceSecMu.Unlock()
		return
	}

	if focCfg.SPKey == nil {
		focCfg.ReloadSPKey()
		if focCfg.SPKey == nil {
			return
		}
	}

	// Snapshot proven epoch before deletion
	dsIDBytes := foc.EncodeBigInt(big.NewInt(int64(gs.LastOnChainDSID)))
	provenBefore, _ := foc.EthCallUint256(ctx, node, focCfg.PDPAddr, foc.BuildCalldata(foc.SigGetLastProvenEpoch, dsIDBytes))
	countBefore, _ := foc.EthCallUint256(ctx, node, focCfg.PDPAddr, foc.BuildCalldata(foc.SigGetActivePieceCount, dsIDBytes))

	// Schedule piece deletion
	pieceIDBig := big.NewInt(int64(s.PieceID))
	sig, err := foc.SignEIP712SchedulePieceRemovals(
		gs.ClientKey, focCfg.FWSSAddr,
		gs.LastClientDSID, []*big.Int{pieceIDBig},
	)
	if err != nil {
		log.Printf("[piece-security] EIP-712 deletion signing failed: %v", err)
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

	ok := foc.SendEthTxConfirmed(ctx, node, focCfg.SPKey, focCfg.PDPAddr, calldata, "piece-security-delete")
	if !ok {
		log.Printf("[piece-security] schedulePieceDeletions failed, will retry")
		return
	}

	log.Printf("[piece-security] deletion scheduled for pieceID=%d", s.PieceID)

	// curio#1039: immediately try to retrieve the piece after deletion scheduled
	// The "no byte-level deletion" behavior means data should still be on disk
	retrieveData, retrieveErr := foc.DownloadPiece(ctx, s.PieceCID)
	if retrieveErr != nil {
		log.Printf("[piece-security] post-delete retrieval: %v (expected if deletion processed)", retrieveErr)
	} else {
		// Retrieval succeeded — verify it's not corrupt
		computedCID, cidErr := foc.CalculatePieceCID(retrieveData)
		if cidErr != nil {
			log.Printf("[piece-security] CRITICAL: post-delete retrieval returned data but CID computation failed: %v", cidErr)
		} else {
			clean := computedCID == s.PieceCID
			assert.Sometimes(clean, "Piece still retrievable after deletion scheduled", map[string]any{
				"pieceCID":    s.PieceCID,
				"computedCID": computedCID,
				"dataLen":     len(retrieveData),
			})
			if !clean {
				log.Printf("[piece-security] CRITICAL: post-delete data CORRUPTED: expected=%s got=%s", s.PieceCID, computedCID)
			} else {
				log.Printf("[piece-security] post-delete retrieval OK (no byte-level deletion confirmed)")
			}
		}
	}

	var provenBeforeU64 uint64
	if provenBefore != nil {
		provenBeforeU64 = provenBefore.Uint64()
	}

	pieceSecMu.Lock()
	pieceSec.ProvenBefore = provenBeforeU64
	pieceSec.CountBefore = countBefore
	pieceSec.State = pieceSecDeleteScheduled
	pieceSecMu.Unlock()
}

// ---------------------------------------------------------------------------
// Phase 4: Verify Delete — piece count decreased, proving continues
// ---------------------------------------------------------------------------

func pieceSecDoVerifyDelete() {
	s := pieceSecSnap()
	gs := griefSnap()
	node := focNode()

	dsIDBytes := foc.EncodeBigInt(big.NewInt(int64(gs.LastOnChainDSID)))

	countAfter, err := foc.EthCallUint256(ctx, node, focCfg.PDPAddr, foc.BuildCalldata(foc.SigGetActivePieceCount, dsIDBytes))
	if err != nil {
		log.Printf("[piece-security] getActivePieceCount failed: %v", err)
		return
	}

	if s.CountBefore != nil && countAfter != nil {
		decreased := countAfter.Cmp(s.CountBefore) < 0
		assert.Sometimes(decreased, "Active piece count decreases after deletion", map[string]any{
			"countBefore": s.CountBefore.String(),
			"countAfter":  countAfter.String(),
			"pieceID":     s.PieceID,
		})
		log.Printf("[piece-security] delete verified: countBefore=%s countAfter=%s", s.CountBefore, countAfter)
	}

	// Check proving still advances
	provenAfter, _ := foc.EthCallUint256(ctx, node, focCfg.PDPAddr, foc.BuildCalldata(foc.SigGetLastProvenEpoch, dsIDBytes))
	if provenAfter != nil {
		advanced := provenAfter.Uint64() >= s.ProvenBefore
		assert.Sometimes(advanced, "Proving continues after piece deletion", map[string]any{
			"provenBefore": s.ProvenBefore,
			"provenAfter":  provenAfter.Uint64(),
		})
	}

	pieceSecMu.Lock()
	pieceSec.CountAfter = countAfter
	pieceSec.State = pieceSecDeleteVerified
	pieceSecMu.Unlock()
}

// ---------------------------------------------------------------------------
// Phase 5: Attack Phase — randomly pick one attack per cycle
// ---------------------------------------------------------------------------

func pieceSecDoAttack() {
	gs := griefSnap()
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
	}

	pick := attacks[rngIntn(len(attacks))]
	log.Printf("[piece-security] attack: %s", pick.name)
	pick.fn(gs, s)

	pieceSecMu.Lock()
	pieceSec.AttacksDone++
	pieceSec.State = pieceSecAttackPhase
	pieceSecMu.Unlock()
}

// attackNonceReplay reuses the nonce from the previous addPieces call.
func attackNonceReplay(gs griefRuntime, s pieceSecRuntime) {
	if s.Nonce == nil || !foc.PingCurio(ctx) {
		return
	}

	// Upload a new piece
	data := make([]byte, 128)
	for i := range data {
		data[i] = byte(random.GetRandom() & 0xFF)
	}
	newCID, err := foc.CalculatePieceCID(data)
	if err != nil {
		return
	}
	if err := foc.UploadPiece(ctx, data, newCID); err != nil {
		return
	}
	_ = foc.WaitForPiece(ctx, newCID)

	parsedCID, err := cid.Decode(newCID)
	if err != nil {
		return
	}

	// Sign with the SAME nonce as the previous add
	sig, err := foc.SignEIP712AddPieces(
		gs.ClientKey, focCfg.FWSSAddr,
		gs.LastClientDSID, s.Nonce, // replayed nonce
		[][]byte{parsedCID.Bytes()}, nil, nil,
	)
	if err != nil {
		return
	}

	extraData := encodeAddPiecesExtraData(s.Nonce, 1, sig)
	_, httpErr := foc.AddPiecesHTTP(ctx, gs.LastOnChainDSID, []string{newCID}, hex.EncodeToString(extraData))

	if httpErr != nil {
		log.Printf("[piece-security] nonce replay rejected at HTTP: %v", httpErr)
		assert.Sometimes(true, "AddPieces nonce replay rejected", map[string]any{
			"replayedNonce": s.Nonce.String(),
		})
	} else {
		// HTTP accepted — check if it actually lands on-chain
		log.Printf("[piece-security] CRITICAL: nonce replay accepted by Curio HTTP — checking on-chain")
		// Even if HTTP accepted, the contract should reject it
		assert.Sometimes(false, "AddPieces nonce replay rejected", map[string]any{
			"replayedNonce": s.Nonce.String(),
			"note":          "HTTP accepted replayed nonce — contract may still reject",
		})
	}
}

// attackCrossDataset signs addPieces for the griefing dataset but submits to the primary FOC dataset.
func attackCrossDataset(gs griefRuntime, _ pieceSecRuntime) {
	if !foc.PingCurio(ctx) {
		return
	}
	focS := snap()
	if focS.OnChainDataSetID == 0 || gs.LastOnChainDSID == 0 {
		return
	}
	if focS.OnChainDataSetID == gs.LastOnChainDSID {
		return // same dataset, not a meaningful test
	}

	data := make([]byte, 128)
	for i := range data {
		data[i] = byte(random.GetRandom() & 0xFF)
	}
	newCID, err := foc.CalculatePieceCID(data)
	if err != nil {
		return
	}
	if err := foc.UploadPiece(ctx, data, newCID); err != nil {
		return
	}
	_ = foc.WaitForPiece(ctx, newCID)

	parsedCID, err := cid.Decode(newCID)
	if err != nil {
		return
	}
	nonce := new(big.Int).SetUint64(random.GetRandom())

	// Sign for GRIEFING dataset
	sig, err := foc.SignEIP712AddPieces(
		gs.ClientKey, focCfg.FWSSAddr,
		gs.LastClientDSID, nonce, // griefing clientDataSetId
		[][]byte{parsedCID.Bytes()}, nil, nil,
	)
	if err != nil {
		return
	}

	extraData := encodeAddPiecesExtraData(nonce, 1, sig)

	// Submit to PRIMARY FOC dataset — signature mismatch
	_, httpErr := foc.AddPiecesHTTP(ctx, focS.OnChainDataSetID, []string{newCID}, hex.EncodeToString(extraData))

	if httpErr != nil {
		log.Printf("[piece-security] cross-dataset injection rejected: %v", httpErr)
		assert.Sometimes(true, "Cross-dataset piece injection rejected", map[string]any{
			"signedFor":   gs.LastOnChainDSID,
			"submittedTo": focS.OnChainDataSetID,
		})
	} else {
		log.Printf("[piece-security] CRITICAL: cross-dataset injection accepted by HTTP")
		assert.Sometimes(false, "Cross-dataset piece injection rejected", map[string]any{
			"signedFor":   gs.LastOnChainDSID,
			"submittedTo": focS.OnChainDataSetID,
		})
	}
}

// attackDoubleDeletion tries to delete the same pieceID that was already deleted in phase 3.
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

	ok := foc.SendEthTxConfirmed(ctx, node, focCfg.SPKey, focCfg.PDPAddr, calldata, "piece-security-double-del")

	if !ok {
		log.Printf("[piece-security] double deletion correctly rejected for pieceID=%d", s.PieceID)
		assert.Sometimes(true, "Double piece deletion rejected", map[string]any{
			"pieceID": s.PieceID,
		})
	} else {
		log.Printf("[piece-security] CRITICAL: double deletion SUCCEEDED for pieceID=%d", s.PieceID)
		assert.Sometimes(false, "Double piece deletion rejected", map[string]any{
			"pieceID": s.PieceID,
			"note":    "same piece deleted twice — accounting bug",
		})
	}
}

// attackNonexistentDelete tries to delete a piece ID that doesn't exist.
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

	ok := foc.SendEthTxConfirmed(ctx, node, focCfg.SPKey, focCfg.PDPAddr, calldata, "piece-security-fake-del")

	if !ok {
		log.Printf("[piece-security] nonexistent piece deletion correctly rejected (fakeID=%s)", fakePieceID)
		assert.Sometimes(true, "Nonexistent piece deletion rejected", map[string]any{
			"fakePieceID": fakePieceID.String(),
		})
	} else {
		log.Printf("[piece-security] CRITICAL: nonexistent piece deletion SUCCEEDED (fakeID=%s)", fakePieceID)
		assert.Sometimes(false, "Nonexistent piece deletion rejected", map[string]any{
			"fakePieceID": fakePieceID.String(),
		})
	}
}

// ---------------------------------------------------------------------------
// Phase 6: Post-Termination Piece Addition Race
// ---------------------------------------------------------------------------

func pieceSecDoTerminate() {
	gs := griefSnap()
	node := focNode()

	if focCfg.SPKey == nil {
		focCfg.ReloadSPKey()
		if focCfg.SPKey == nil {
			return
		}
	}
	if !foc.PingCurio(ctx) {
		return
	}

	// Terminate the griefing dataset
	calldata := foc.BuildCalldata(foc.SigTerminateService,
		foc.EncodeBigInt(gs.LastClientDSID),
	)

	ok := foc.SendEthTxConfirmed(ctx, node, focCfg.SPKey, focCfg.FWSSAddr, calldata, "piece-security-terminate")
	if !ok {
		log.Printf("[piece-security] terminateService failed, will retry")
		return
	}

	log.Printf("[piece-security] service terminated for dataset=%d", gs.LastOnChainDSID)

	// THE KEY TEST: immediately try to add a piece after termination
	data := make([]byte, 128)
	for i := range data {
		data[i] = byte(random.GetRandom() & 0xFF)
	}
	newCID, err := foc.CalculatePieceCID(data)
	if err != nil {
		log.Printf("[piece-security] post-term CID calc failed: %v", err)
		pieceSecMu.Lock()
		pieceSec.TermDataSetID = gs.LastOnChainDSID
		pieceSec.TermClientDSID = gs.LastClientDSID
		pieceSec.State = pieceSecTerminated
		pieceSecMu.Unlock()
		return
	}

	_ = foc.UploadPiece(ctx, data, newCID)
	_ = foc.WaitForPiece(ctx, newCID)

	parsedCID, err := cid.Decode(newCID)
	if err != nil {
		pieceSecMu.Lock()
		pieceSec.TermDataSetID = gs.LastOnChainDSID
		pieceSec.TermClientDSID = gs.LastClientDSID
		pieceSec.State = pieceSecTerminated
		pieceSecMu.Unlock()
		return
	}

	nonce := new(big.Int).SetUint64(random.GetRandom())
	sig, err := foc.SignEIP712AddPieces(
		gs.ClientKey, focCfg.FWSSAddr,
		gs.LastClientDSID, nonce,
		[][]byte{parsedCID.Bytes()}, nil, nil,
	)
	if err != nil {
		pieceSecMu.Lock()
		pieceSec.TermDataSetID = gs.LastOnChainDSID
		pieceSec.TermClientDSID = gs.LastClientDSID
		pieceSec.State = pieceSecTerminated
		pieceSecMu.Unlock()
		return
	}

	extraData := encodeAddPiecesExtraData(nonce, 1, sig)
	_, httpErr := foc.AddPiecesHTTP(ctx, gs.LastOnChainDSID, []string{newCID}, hex.EncodeToString(extraData))

	if httpErr != nil {
		log.Printf("[piece-security] post-termination add rejected: %v", httpErr)
		assert.Sometimes(true, "Piece addition blocked after termination", map[string]any{
			"dataSetID": gs.LastOnChainDSID,
		})
	} else {
		log.Printf("[piece-security] CRITICAL: post-termination add ACCEPTED for dataset=%d", gs.LastOnChainDSID)
		assert.Sometimes(false, "Piece addition blocked after termination", map[string]any{
			"dataSetID": gs.LastOnChainDSID,
			"note":      "pieces added to dying dataset — orphan risk",
		})
	}

	pieceSecMu.Lock()
	pieceSec.TermDataSetID = gs.LastOnChainDSID
	pieceSec.TermClientDSID = gs.LastClientDSID
	pieceSec.State = pieceSecTerminated
	pieceSecMu.Unlock()
}

// ---------------------------------------------------------------------------
// Phase 7: Cleanup — delete dataset, re-create, reset
// ---------------------------------------------------------------------------

func pieceSecDoCleanup() {
	s := pieceSecSnap()
	gs := griefSnap()
	node := focNode()

	if focCfg.SPKey == nil {
		return
	}

	// Delete the terminated dataset
	if s.TermDataSetID > 0 && s.TermClientDSID != nil {
		sig, err := foc.SignEIP712DeleteDataSet(gs.ClientKey, focCfg.FWSSAddr, s.TermClientDSID)
		if err != nil {
			log.Printf("[piece-security] deleteDataSet EIP-712 signing failed: %v", err)
			// Don't block — reset anyway
		} else {
			extraData := encodeBytes(sig)
			calldata := foc.BuildCalldata(foc.SigDeleteDataSet,
				foc.EncodeBigInt(big.NewInt(int64(s.TermDataSetID))),
				foc.EncodeBigInt(big.NewInt(64)),
				extraData,
			)
			sent := foc.SendEthTxConfirmed(ctx, node, focCfg.SPKey, focCfg.PDPAddr, calldata, "piece-security-cleanup")
			if sent {
				log.Printf("[piece-security] dataset %d deleted", s.TermDataSetID)
			} else {
				log.Printf("[piece-security] dataset %d delete failed (endEpoch may not have passed yet), will retry", s.TermDataSetID)
				return // retry next invocation
			}
		}
	}

	// Re-create a new dataset for the griefing runtime via probeEmptyDatasetFee flow
	// This is handled by the griefing probe on its next invocation once it detects
	// the dataset was deleted. We just need to reset griefRT state.
	griefMu.Lock()
	griefRT.LastOnChainDSID = 0
	griefRT.LastClientDSID = nil
	griefRT.DSCreated = 0
	griefMu.Unlock()

	pieceSecMu.Lock()
	pieceSec.Cycles++
	cycles := pieceSec.Cycles
	attacks := pieceSec.AttacksDone
	pieceSec.State = pieceSecInit
	pieceSec.PieceCID = ""
	pieceSec.PieceID = 0
	pieceSec.Nonce = nil
	pieceSec.CountBefore = nil
	pieceSec.CountAfter = nil
	pieceSec.ProvenBefore = 0
	pieceSec.TermDataSetID = 0
	pieceSec.TermClientDSID = nil
	pieceSecMu.Unlock()

	log.Printf("[piece-security] cycle %d complete (attacks=%d), resetting to Init", cycles, attacks)
	assert.Sometimes(true, "Piece security scenario cycle completes", map[string]any{
		"cycles":  cycles,
		"attacks": attacks,
	})
}

// ---------------------------------------------------------------------------
// Progress
// ---------------------------------------------------------------------------

func logPieceSecProgress() {
	s := pieceSecSnap()
	log.Printf("[piece-security] state=%s cycles=%d attacks=%d pieceCID=%s pieceID=%d",
		s.State, s.Cycles, s.AttacksDone, s.PieceCID, s.PieceID)
}
