package main

import (
	"encoding/hex"
	"log"
	"math/big"
	"strings"
	"sync"

	"workload/internal/foc"

	"github.com/antithesishq/antithesis-sdk-go/assert"
	"github.com/antithesishq/antithesis-sdk-go/random"
	"github.com/filecoin-project/lotus/api"
	"github.com/ipfs/go-cid"
)

// ===========================================================================
// FOC Lifecycle — Sequential State Machine
//
// The lifecycle executes setup steps in strict order:
//   Init → Approved → Deposited → OperatorApproved → DataSetCreated → Ready
//
// DoFOCLifecycle is called from the deck and advances one step per invocation.
// Steady-state vectors (upload, add pieces, monitor, transfer, settle, withdraw)
// are independent deck entries that only fire once state == Ready.
//
// Safety invariants (assert.Always) live in the foc-sidecar, not here.
// Vectors here use assert.Sometimes — under fault injection any tx can fail.
// ===========================================================================

const focUSDFCUnit = 1e18

// ---------------------------------------------------------------------------
// Lifecycle state
// ---------------------------------------------------------------------------

type focLifecycleState int

const (
	focStateInit focLifecycleState = iota
	focStateApproved
	focStateDeposited
	focStateOperatorApproved
	focStateDataSetCreated
	focStateReady
)

func (s focLifecycleState) String() string {
	switch s {
	case focStateInit:
		return "Init"
	case focStateApproved:
		return "Approved"
	case focStateDeposited:
		return "Deposited"
	case focStateOperatorApproved:
		return "OperatorApproved"
	case focStateDataSetCreated:
		return "DataSetCreated"
	case focStateReady:
		return "Ready"
	default:
		return "Unknown"
	}
}

var (
	focState   focRuntime
	focStateMu sync.Mutex
)

type focRuntime struct {
	State           focLifecycleState
	ClientDataSetID *big.Int
	OnChainDataSetID int

	UploadedPieces []pieceRef // uploaded to Curio, not yet added on-chain
	AddedPieces    []pieceRef // confirmed on-chain in proofset

	TerminationInitiated bool // true after terminateService has been called
}

type pieceRef struct {
	PieceCID string
	PieceID  int
}

// snap takes a snapshot of focState under the lock.
func snap() focRuntime {
	focStateMu.Lock()
	defer focStateMu.Unlock()
	s := focState
	s.UploadedPieces = append([]pieceRef(nil), focState.UploadedPieces...)
	s.AddedPieces = append([]pieceRef(nil), focState.AddedPieces...)
	return s
}

// requireReady returns a state snapshot and true if the lifecycle is Ready.
func requireReady() (focRuntime, bool) {
	if focCfg == nil {
		return focRuntime{}, false
	}
	s := snap()
	return s, s.State == focStateReady
}

// returnPiece puts a piece back on the uploaded queue (used on failure paths).
func returnPiece(p pieceRef) {
	focStateMu.Lock()
	focState.UploadedPieces = append(focState.UploadedPieces, p)
	focStateMu.Unlock()
}

// focNode returns a lotus node (not forest) for FOC transactions.
func focNode() api.FullNode {
	if n, ok := nodes["lotus0"]; ok {
		return n
	}
	for name, n := range nodes {
		if strings.HasPrefix(name, "lotus") {
			return n
		}
	}
	_, n := pickNode()
	return n
}

// ---------------------------------------------------------------------------
// DoFOCLifecycle — State Machine (one step per invocation)
// ---------------------------------------------------------------------------

func DoFOCLifecycle() {
	if focCfg == nil || focCfg.ClientKey == nil {
		return
	}

	focStateMu.Lock()
	currentState := focState.State
	focStateMu.Unlock()

	switch currentState {
	case focStateInit:
		doStepApprove()
	case focStateApproved:
		doStepDeposit()
	case focStateDeposited:
		doStepApproveOperator()
	case focStateOperatorApproved:
		doStepCreateDataSet()
	case focStateDataSetCreated:
		doStepWaitForDataSet()
	case focStateReady:
		logFOCProgress()
	}
}

// doStepApprove submits ERC-20 approve(FilecoinPay, MaxUint256) and waits for confirmation.
func doStepApprove() {
	if focCfg.USDFCAddr == nil || focCfg.FilPayAddr == nil {
		return
	}

	node := focNode()

	maxUint256 := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	calldata := foc.BuildCalldata(foc.SigApprove,
		foc.EncodeAddress(focCfg.FilPayAddr),
		foc.EncodeBigInt(maxUint256),
	)

	log.Printf("[foc-lifecycle] state=Init → submitting ERC-20 approve")
	ok := foc.SendEthTxConfirmed(ctx, node, focCfg.ClientKey, focCfg.USDFCAddr, calldata, "foc-approve")
	if !ok {
		log.Printf("[foc-lifecycle] approve failed, will retry")
		return
	}

	log.Printf("[foc-lifecycle] approve confirmed on-chain")
	assert.Sometimes(true, "USDFC approve for FilecoinPay succeeds", nil)

	focStateMu.Lock()
	focState.State = focStateApproved
	focStateMu.Unlock()
}

// doStepDeposit submits deposit(USDFC, client, 500 USDFC) and waits for confirmation.
func doStepDeposit() {
	if focCfg.USDFCAddr == nil || focCfg.FilPayAddr == nil || focCfg.ClientEthAddr == nil {
		return
	}

	node := focNode()

	amount := new(big.Int).Mul(big.NewInt(500), big.NewInt(focUSDFCUnit))
	calldata := foc.BuildCalldata(foc.SigDeposit,
		foc.EncodeAddress(focCfg.USDFCAddr),
		foc.EncodeAddress(focCfg.ClientEthAddr),
		foc.EncodeBigInt(amount),
	)

	log.Printf("[foc-lifecycle] state=Approved → submitting deposit amount=%s", amount)
	ok := foc.SendEthTxConfirmed(ctx, node, focCfg.ClientKey, focCfg.FilPayAddr, calldata, "foc-deposit")
	if !ok {
		log.Printf("[foc-lifecycle] deposit failed, will retry")
		return
	}

	// Verify funds arrived on-chain
	funds := foc.ReadAccountFunds(ctx, node, focCfg.FilPayAddr, focCfg.USDFCAddr, focCfg.ClientEthAddr)
	log.Printf("[foc-lifecycle] deposit confirmed: funds=%s", funds)
	assert.Sometimes(true, "USDFC deposit into FilecoinPay succeeds", map[string]any{
		"funds": funds.String(),
	})

	focStateMu.Lock()
	focState.State = focStateDeposited
	focStateMu.Unlock()
}

// doStepApproveOperator submits setOperatorApproval(USDFC, FWSS, true, ...) and waits.
func doStepApproveOperator() {
	if focCfg.FilPayAddr == nil || focCfg.FWSSAddr == nil || focCfg.USDFCAddr == nil {
		return
	}

	node := focNode()

	maxUint256 := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	maxLockupPeriod := big.NewInt(86400)

	calldata := foc.BuildCalldata(foc.SigSetOpApproval,
		foc.EncodeAddress(focCfg.USDFCAddr),
		foc.EncodeAddress(focCfg.FWSSAddr),
		foc.EncodeBool(true),
		foc.EncodeBigInt(maxUint256),
		foc.EncodeBigInt(maxUint256),
		foc.EncodeBigInt(maxLockupPeriod),
	)

	log.Printf("[foc-lifecycle] state=Deposited → submitting operator approval")
	ok := foc.SendEthTxConfirmed(ctx, node, focCfg.ClientKey, focCfg.FilPayAddr, calldata, "foc-approve-op")
	if !ok {
		log.Printf("[foc-lifecycle] operator approval failed, will retry")
		return
	}

	log.Printf("[foc-lifecycle] operator approval confirmed on-chain")
	assert.Sometimes(true, "FWSS operator approval succeeds", nil)

	focStateMu.Lock()
	focState.State = focStateOperatorApproved
	focStateMu.Unlock()
}

// doStepCreateDataSet creates a dataset via Curio PDP HTTP API.
func doStepCreateDataSet() {
	if focCfg.FWSSAddr == nil || focCfg.PDPAddr == nil {
		return
	}

	// Retry SP key load if not yet available
	if focCfg.SPKey == nil || focCfg.SPEthAddr == nil {
		focCfg.ReloadSPKey()
		if focCfg.SPKey == nil {
			log.Printf("[foc-lifecycle] SP key not available yet, will retry")
			return
		}
	}

	clientDataSetId := new(big.Int).SetUint64(random.GetRandom())
	metadataKeys := []string{"source"}
	metadataValues := []string{"antithesis-stress"}
	payee := focCfg.SPEthAddr

	sig, err := foc.SignEIP712CreateDataSet(
		focCfg.ClientKey, focCfg.FWSSAddr,
		clientDataSetId, payee,
		metadataKeys, metadataValues,
	)
	if err != nil {
		log.Printf("[foc-lifecycle] EIP-712 signing failed: %v", err)
		return
	}

	extraData := encodeCreateDataSetExtra(focCfg.ClientEthAddr, clientDataSetId, metadataKeys, metadataValues, sig)
	recordKeeper := "0x" + hex.EncodeToString(focCfg.FWSSAddr)

	log.Printf("[foc-lifecycle] state=OperatorApproved → creating dataset clientDataSetId=%s", clientDataSetId)
	txHash, err := foc.CreateDataSetHTTP(ctx, recordKeeper, hex.EncodeToString(extraData))
	if err != nil {
		log.Printf("[foc-lifecycle] CreateDataSetHTTP failed: %v", err)
		return
	}

	log.Printf("[foc-lifecycle] dataset tx submitted via Curio: txHash=%s", txHash)

	onChainID, err := foc.WaitForDataSetCreation(ctx, txHash)
	if err != nil {
		log.Printf("[foc-lifecycle] WaitForDataSetCreation failed: %v", err)
		return
	}

	log.Printf("[foc-lifecycle] dataset created: clientDataSetId=%s onChainDataSetID=%d", clientDataSetId, onChainID)
	assert.Sometimes(true, "FOC dataset creation completes end-to-end", map[string]any{
		"clientDataSetId":  clientDataSetId.String(),
		"onChainDataSetID": onChainID,
	})

	focStateMu.Lock()
	focState.ClientDataSetID = clientDataSetId
	focState.OnChainDataSetID = onChainID
	focState.State = focStateDataSetCreated
	focStateMu.Unlock()
}

// doStepWaitForDataSet transitions to Ready. The dataset is already confirmed
// by WaitForDataSetCreation, so this is just a final check.
func doStepWaitForDataSet() {
	s := snap()
	if s.OnChainDataSetID == 0 {
		log.Printf("[foc-lifecycle] dataset ID still 0, waiting...")
		return
	}

	log.Printf("[foc-lifecycle] state=Ready — dataset %d active, lifecycle setup complete", s.OnChainDataSetID)

	focStateMu.Lock()
	focState.State = focStateReady
	focStateMu.Unlock()
}

// ---------------------------------------------------------------------------
// Steady-State Vectors (only fire when lifecycle is Ready)
// ---------------------------------------------------------------------------

// DoFOCUploadPiece uploads random data to Curio's PDP API.
func DoFOCUploadPiece() {
	if _, ok := requireReady(); !ok {
		return
	}

	if !foc.PingCurio(ctx) {
		log.Printf("[foc-upload] curio not reachable, skipping")
		return
	}

	size := 128 + rngIntn(897)
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(random.GetRandom() & 0xFF)
	}

	pieceCID, err := foc.CalculatePieceCID(data)
	if err != nil {
		log.Printf("[foc-upload] CalculatePieceCID failed: %v", err)
		return
	}

	err = foc.UploadPiece(ctx, data, pieceCID)
	if err != nil {
		log.Printf("[foc-upload] UploadPiece failed: %v", err)
		return
	}

	err = foc.WaitForPiece(ctx, pieceCID)
	if err != nil {
		log.Printf("[foc-upload] WaitForPiece failed: %v", err)
		return
	}

	log.Printf("[foc-upload] piece uploaded and indexed: %s (%d bytes)", pieceCID, size)
	assert.Sometimes(true, "piece upload to Curio succeeds", map[string]any{
		"pieceCID": pieceCID,
		"size":     size,
	})

	focStateMu.Lock()
	focState.UploadedPieces = append(focState.UploadedPieces, pieceRef{PieceCID: pieceCID})
	focStateMu.Unlock()
}

// DoFOCAddPieces adds uploaded pieces to the on-chain proofset via Curio HTTP API.
func DoFOCAddPieces() {
	if focCfg == nil || focCfg.ClientKey == nil || focCfg.FWSSAddr == nil {
		return
	}

	// Grab one uploaded piece
	focStateMu.Lock()
	ready := focState.State == focStateReady && len(focState.UploadedPieces) > 0
	var piece pieceRef
	dataSetID := focState.OnChainDataSetID
	clientDataSetID := focState.ClientDataSetID
	if ready {
		piece = focState.UploadedPieces[0]
		focState.UploadedPieces = focState.UploadedPieces[1:]
	}
	focStateMu.Unlock()

	if !ready {
		return
	}

	if dataSetID == 0 {
		log.Printf("[foc-add-pieces] no on-chain dataset ID yet, putting piece back")
		returnPiece(piece)
		return
	}

	nonce := new(big.Int).SetUint64(random.GetRandom())

	// Decode CID string to raw binary bytes for EIP-712 signing.
	// The contract verifies against binary CID bytes, not the string representation.
	parsedCID, err := cid.Decode(piece.PieceCID)
	if err != nil {
		log.Printf("[foc-add-pieces] CID decode failed for %s: %v", piece.PieceCID, err)
		returnPiece(piece)
		return
	}
	cidBytes := parsedCID.Bytes()

	sig, err := foc.SignEIP712AddPieces(
		focCfg.ClientKey, focCfg.FWSSAddr,
		clientDataSetID, nonce,
		[][]byte{cidBytes},
		nil, nil,
	)
	if err != nil {
		log.Printf("[foc-add-pieces] EIP-712 signing failed: %v", err)
		returnPiece(piece)
		return
	}

	extraData := encodeAddPiecesExtraData(nonce, 1, sig)

	txHash, err := foc.AddPiecesHTTP(ctx, dataSetID, []string{piece.PieceCID}, hex.EncodeToString(extraData))
	if err != nil {
		log.Printf("[foc-add-pieces] AddPiecesHTTP failed: %v", err)
		returnPiece(piece)
		return
	}

	pieceIDs, err := foc.WaitForPieceAddition(ctx, dataSetID, txHash)
	if err != nil {
		log.Printf("[foc-add-pieces] WaitForPieceAddition failed: %v", err)
		returnPiece(piece)
		return
	}

	log.Printf("[foc-add-pieces] piece added: cid=%s pieceIDs=%v", piece.PieceCID, pieceIDs)
	assert.Sometimes(true, "pieces added to proofset", map[string]any{
		"pieceCID": piece.PieceCID,
		"pieceIDs": pieceIDs,
	})

	focStateMu.Lock()
	if len(pieceIDs) > 0 {
		for _, pid := range pieceIDs {
			focState.AddedPieces = append(focState.AddedPieces, pieceRef{
				PieceCID: piece.PieceCID,
				PieceID:  pid,
			})
		}
	} else {
		// Curio confirmed the piece was added but didn't return IDs.
		// Track by CID so retrieval verification can still work.
		focState.AddedPieces = append(focState.AddedPieces, pieceRef{
			PieceCID: piece.PieceCID,
			PieceID:  0,
		})
	}
	focStateMu.Unlock()
}

// DoFOCMonitorProofSet queries proofset health and logs balances.
func DoFOCMonitorProofSet() {
	if focCfg == nil || focCfg.PDPAddr == nil {
		return
	}

	node := focNode()

	// Read USDFC balances
	if focCfg.USDFCAddr != nil && focCfg.FilPayAddr != nil && focCfg.ClientEthAddr != nil {
		funds := foc.ReadAccountFunds(ctx, node, focCfg.FilPayAddr, focCfg.USDFCAddr, focCfg.ClientEthAddr)
		lockup := foc.ReadAccountLockup(ctx, node, focCfg.FilPayAddr, focCfg.USDFCAddr, focCfg.ClientEthAddr)
		log.Printf("[foc-monitor] client FilecoinPay: funds=%s lockup=%s", funds, lockup)

		balCalldata := foc.BuildCalldata(foc.SigBalanceOf, foc.EncodeAddress(focCfg.ClientEthAddr))
		clientBal, err := foc.EthCallUint256(ctx, node, focCfg.USDFCAddr, balCalldata)
		if err == nil {
			log.Printf("[foc-monitor] client USDFC balance=%s", clientBal)
		}
	}

	// Query proofset on-chain state if we have a dataset
	s := snap()
	if s.State != focStateReady || s.OnChainDataSetID == 0 {
		return
	}

	dsIDBytes := foc.EncodeBigInt(big.NewInt(int64(s.OnChainDataSetID)))

	live, err := foc.EthCallBool(ctx, node, focCfg.PDPAddr, foc.BuildCalldata(foc.SigDataSetLive, dsIDBytes))
	if err != nil {
		log.Printf("[foc-monitor] dataSetLive call failed: %v", err)
		return
	}

	activePieces, _ := foc.EthCallUint256(ctx, node, focCfg.PDPAddr, foc.BuildCalldata(foc.SigGetActivePieceCount, dsIDBytes))
	nextChallenge, _ := foc.EthCallUint256(ctx, node, focCfg.PDPAddr, foc.BuildCalldata(foc.SigGetNextChallengeEpoch, dsIDBytes))

	log.Printf("[foc-monitor] dataset=%d live=%v activePieces=%s nextChallenge=%s",
		s.OnChainDataSetID, live, activePieces, nextChallenge)

	assert.Sometimes(live, "proofset is live", map[string]any{
		"dataSetID": s.OnChainDataSetID,
	})
	if activePieces != nil {
		assert.Sometimes(activePieces.Sign() > 0, "proofset has active pieces", map[string]any{
			"dataSetID":    s.OnChainDataSetID,
			"activePieces": activePieces.String(),
		})
	}
}

// DoFOCTransfer performs an ERC-20 USDFC transfer between client and deployer.
func DoFOCTransfer() {
	if _, ok := requireReady(); !ok {
		return
	}
	if focCfg.ClientKey == nil || focCfg.DeployerEthAddr == nil || focCfg.USDFCAddr == nil {
		return
	}

	node := focNode()

	amount := new(big.Int).Mul(
		big.NewInt(int64(rngIntn(3)+1)),
		big.NewInt(focUSDFCUnit/100),
	)

	calldata := foc.BuildCalldata(foc.SigTransfer,
		foc.EncodeAddress(focCfg.DeployerEthAddr),
		foc.EncodeBigInt(amount),
	)

	ok := foc.SendEthTx(ctx, node, focCfg.ClientKey, focCfg.USDFCAddr, calldata, "foc-transfer")

	log.Printf("[foc-transfer] amount=%s ok=%v", amount, ok)
	assert.Sometimes(ok, "USDFC transfer succeeds", map[string]any{
		"amount": amount.String(),
	})
}

// DoFOCSettle settles a payment rail on FilecoinPay.
func DoFOCSettle() {
	if _, ok := requireReady(); !ok {
		return
	}
	if focCfg.ClientKey == nil || focCfg.FilPayAddr == nil || focCfg.USDFCAddr == nil {
		return
	}

	node := focNode()

	railCalldata := foc.BuildCalldata(foc.SigGetRailsByPayer,
		foc.EncodeAddress(focCfg.ClientEthAddr),
		foc.EncodeAddress(focCfg.USDFCAddr),
		foc.EncodeBigInt(big.NewInt(0)),
		foc.EncodeBigInt(big.NewInt(10)),
	)

	result, err := foc.EthCallRaw(ctx, node, focCfg.FilPayAddr, railCalldata)
	if err != nil {
		log.Printf("[foc-settle] getRailsForPayerAndToken failed: %v", err)
		return
	}

	if len(result) < 96 {
		log.Printf("[foc-settle] no rails found (result too short: %d bytes)", len(result))
		return
	}

	arrayLen := new(big.Int).SetBytes(result[32:64])
	if arrayLen.Sign() == 0 {
		log.Printf("[foc-settle] no rails found")
		return
	}

	railID := new(big.Int).SetBytes(result[64:96])

	head, err := node.ChainHead(ctx)
	if err != nil {
		log.Printf("[foc-settle] ChainHead failed: %v", err)
		return
	}
	untilEpoch := big.NewInt(int64(head.Height()))

	settleCalldata := foc.BuildCalldata(foc.SigSettleRail,
		foc.EncodeBigInt(railID),
		foc.EncodeBigInt(untilEpoch),
	)

	ok := foc.SendEthTx(ctx, node, focCfg.ClientKey, focCfg.FilPayAddr, settleCalldata, "foc-settle")

	log.Printf("[foc-settle] railID=%s untilEpoch=%s ok=%v", railID, untilEpoch, ok)
	assert.Sometimes(ok, "payment rail settlement succeeds", map[string]any{
		"railID":     railID.String(),
		"untilEpoch": untilEpoch.String(),
	})
}

// DoFOCWithdraw withdraws a small portion of available USDFC from FilecoinPay.
func DoFOCWithdraw() {
	if _, ok := requireReady(); !ok {
		return
	}
	if focCfg.ClientKey == nil || focCfg.FilPayAddr == nil ||
		focCfg.USDFCAddr == nil || focCfg.ClientEthAddr == nil {
		return
	}

	node := focNode()

	funds := foc.ReadAccountFunds(ctx, node, focCfg.FilPayAddr, focCfg.USDFCAddr, focCfg.ClientEthAddr)
	if funds == nil || funds.Sign() == 0 {
		return
	}

	pct := 1 + rngIntn(5)
	amount := new(big.Int).Mul(funds, big.NewInt(int64(pct)))
	amount.Div(amount, big.NewInt(100))
	if amount.Sign() == 0 {
		return
	}

	calldata := foc.BuildCalldata(foc.SigWithdraw,
		foc.EncodeAddress(focCfg.USDFCAddr),
		foc.EncodeBigInt(amount),
	)

	ok := foc.SendEthTx(ctx, node, focCfg.ClientKey, focCfg.FilPayAddr, calldata, "foc-withdraw")

	log.Printf("[foc-withdraw] amount=%s (of %s, %d%%) ok=%v", amount, funds, pct, ok)
	assert.Sometimes(ok, "USDFC withdrawal from FilecoinPay succeeds", map[string]any{
		"amount": amount.String(),
	})
}

// DoFOCDeletePiece schedules deletion of a piece from the proofset.
func DoFOCDeletePiece() {
	if focCfg == nil || focCfg.ClientKey == nil ||
		focCfg.FWSSAddr == nil || focCfg.PDPAddr == nil {
		return
	}
	if focCfg.SPKey == nil {
		focCfg.ReloadSPKey()
		if focCfg.SPKey == nil {
			return
		}
	}

	focStateMu.Lock()
	ready := focState.State == focStateReady && len(focState.AddedPieces) > 0
	var piece pieceRef
	dsID := focState.OnChainDataSetID
	clientDataSetID := focState.ClientDataSetID
	if ready {
		piece = focState.AddedPieces[len(focState.AddedPieces)-1]
		focState.AddedPieces = focState.AddedPieces[:len(focState.AddedPieces)-1]
	}
	focStateMu.Unlock()

	if !ready || dsID == 0 {
		return
	}

	node := focNode()

	pieceIDBig := big.NewInt(int64(piece.PieceID))
	sig, err := foc.SignEIP712SchedulePieceRemovals(
		focCfg.ClientKey, focCfg.FWSSAddr,
		clientDataSetID, []*big.Int{pieceIDBig},
	)
	if err != nil {
		log.Printf("[foc-delete-piece] EIP-712 signing failed: %v", err)
		return
	}

	extraData := encodeBytes(sig)
	calldata := foc.BuildCalldata(foc.SigSchedulePieceDeletions,
		foc.EncodeBigInt(big.NewInt(int64(dsID))),
		foc.EncodeBigInt(big.NewInt(96)),
		foc.EncodeBigInt(big.NewInt(160)),
		foc.EncodeBigInt(big.NewInt(1)),
		foc.EncodeBigInt(pieceIDBig),
		extraData,
	)

	ok := foc.SendEthTx(ctx, node, focCfg.SPKey, focCfg.PDPAddr, calldata, "foc-delete-piece")

	log.Printf("[foc-delete-piece] pieceID=%d cid=%s ok=%v", piece.PieceID, piece.PieceCID, ok)
	assert.Sometimes(ok, "piece deletion scheduled", map[string]any{
		"pieceID":  piece.PieceID,
		"pieceCID": piece.PieceCID,
	})
}

// DoFOCDeleteDataSet deletes the entire dataset following the proper FWSS
// termination pipeline:
//   Phase 1: Call FWSS.terminateService(clientDataSetId) to initiate termination.
//   Phase 2: Call PDPVerifier.deleteDataSet(dataSetId, extraData) after the
//            service termination epoch has passed.
//
// Each deck invocation advances one phase. Phase 2 may need multiple attempts
// while waiting for the PdpEndEpoch to pass (the contract will revert if early).
func DoFOCDeleteDataSet() {
	s, ok := requireReady()
	if !ok || s.OnChainDataSetID == 0 {
		return
	}
	if focCfg.ClientKey == nil || focCfg.FWSSAddr == nil || focCfg.PDPAddr == nil {
		return
	}
	if focCfg.SPKey == nil {
		focCfg.ReloadSPKey()
		if focCfg.SPKey == nil {
			return
		}
	}

	node := focNode()

	// Phase 1: Initiate service termination via FWSS
	if !s.TerminationInitiated {
		calldata := foc.BuildCalldata(foc.SigTerminateService,
			foc.EncodeBigInt(s.ClientDataSetID),
		)

		ok := foc.SendEthTxConfirmed(ctx, node, focCfg.SPKey, focCfg.FWSSAddr, calldata, "foc-terminate-svc")
		if !ok {
			log.Printf("[foc-delete-ds] terminateService failed for clientDataSetId=%s, will retry", s.ClientDataSetID)
			return
		}

		log.Printf("[foc-delete-ds] service termination initiated: clientDataSetId=%s dataSetID=%d",
			s.ClientDataSetID, s.OnChainDataSetID)
		assert.Sometimes(true, "FWSS service termination initiated", map[string]any{
			"clientDataSetId": s.ClientDataSetID.String(),
			"dataSetID":       s.OnChainDataSetID,
		})

		focStateMu.Lock()
		focState.TerminationInitiated = true
		focStateMu.Unlock()
		return
	}

	// Phase 2: Delete the dataset on PDPVerifier (requires termination epoch to have passed)
	sig, err := foc.SignEIP712DeleteDataSet(
		focCfg.ClientKey, focCfg.FWSSAddr, s.ClientDataSetID,
	)
	if err != nil {
		log.Printf("[foc-delete-ds] EIP-712 signing failed: %v", err)
		return
	}

	extraData := encodeBytes(sig)
	calldata := foc.BuildCalldata(foc.SigDeleteDataSet,
		foc.EncodeBigInt(big.NewInt(int64(s.OnChainDataSetID))),
		foc.EncodeBigInt(big.NewInt(64)),
		extraData,
	)

	sent := foc.SendEthTxConfirmed(ctx, node, focCfg.SPKey, focCfg.PDPAddr, calldata, "foc-delete-ds")

	log.Printf("[foc-delete-ds] dataSetID=%d ok=%v", s.OnChainDataSetID, sent)
	assert.Sometimes(sent, "dataset deletion succeeds", map[string]any{
		"dataSetID": s.OnChainDataSetID,
	})

	if sent {
		focStateMu.Lock()
		focState.State = focStateInit
		focState.OnChainDataSetID = 0
		focState.ClientDataSetID = nil
		focState.AddedPieces = nil
		focState.UploadedPieces = nil
		focState.TerminationInitiated = false
		focStateMu.Unlock()
	}
}

// DoFOCRetrieveAndVerify downloads a piece from Curio and verifies that the
// retrieved data produces the same PieceCID as the original upload.
func DoFOCRetrieveAndVerify() {
	if _, ok := requireReady(); !ok {
		return
	}

	focStateMu.Lock()
	nPieces := len(focState.AddedPieces)
	var piece pieceRef
	if nPieces > 0 {
		piece = focState.AddedPieces[rngIntn(nPieces)]
	}
	focStateMu.Unlock()

	if nPieces == 0 {
		return
	}

	data, err := foc.DownloadPiece(ctx, piece.PieceCID)
	if err != nil {
		log.Printf("[foc-retrieve] download failed for %s: %v", piece.PieceCID, err)
		return
	}

	computedCID, err := foc.CalculatePieceCID(data)
	if err != nil {
		log.Printf("[foc-retrieve] CalculatePieceCID failed for %s: %v", piece.PieceCID, err)
		return
	}

	match := computedCID == piece.PieceCID

	assert.Sometimes(match, "piece retrieval integrity verified", map[string]any{
		"pieceCID":    piece.PieceCID,
		"computedCID": computedCID,
		"dataLen":     len(data),
	})

	if !match {
		log.Printf("[foc-retrieve] INTEGRITY MISMATCH: expected=%s computed=%s len=%d",
			piece.PieceCID, computedCID, len(data))
	} else {
		log.Printf("[foc-retrieve] verified: cid=%s len=%d", piece.PieceCID, len(data))
	}
}

// ---------------------------------------------------------------------------
// Progress Summary
// ---------------------------------------------------------------------------

func logFOCProgress() {
	s := snap()
	spLoaded := focCfg != nil && focCfg.SPKey != nil

	if focCfg != nil && !spLoaded {
		focCfg.ReloadSPKey()
		spLoaded = focCfg.SPKey != nil
		if spLoaded {
			log.Printf("[foc-progress] SP key loaded on retry: SPEthAddr=%x", focCfg.SPEthAddr)
		}
	}

	log.Printf("[foc-progress] state=%s onChainDS=%d uploaded=%d added=%d spKey=%v",
		s.State, s.OnChainDataSetID, len(s.UploadedPieces), len(s.AddedPieces), spLoaded)

	if !spLoaded {
		log.Printf("[foc-progress] BLOCKED: SP key not available")
	}
}

// ---------------------------------------------------------------------------
// ABI Encoding Helpers
// ---------------------------------------------------------------------------

func encodeCreateDataSetExtra(payer []byte, clientDataSetId *big.Int, keys, values []string, sig []byte) []byte {
	headSlots := 5
	headSize := headSlots * 32

	keysEncoded := encodeStringArray(keys)
	valuesEncoded := encodeStringArray(values)
	sigEncoded := encodeBytes(sig)

	keysOffset := big.NewInt(int64(headSize))
	valuesOffset := big.NewInt(int64(headSize + len(keysEncoded)))
	sigOffset := big.NewInt(int64(headSize + len(keysEncoded) + len(valuesEncoded)))

	var buf []byte
	buf = append(buf, foc.EncodeAddress(payer)...)
	buf = append(buf, foc.EncodeBigInt(clientDataSetId)...)
	buf = append(buf, foc.EncodeBigInt(keysOffset)...)
	buf = append(buf, foc.EncodeBigInt(valuesOffset)...)
	buf = append(buf, foc.EncodeBigInt(sigOffset)...)
	buf = append(buf, keysEncoded...)
	buf = append(buf, valuesEncoded...)
	buf = append(buf, sigEncoded...)

	return buf
}

func encodeAddPiecesExtraData(nonce *big.Int, numPieces int, sig []byte) []byte {
	// ABI-encode: (uint256 nonce, string[][] keys, string[][] values, bytes sig)
	// The FWSS contract requires keys.length == values.length == numPieces.
	keysEncoded := abiEncodeEmptyStringArrayArray(numPieces)
	valsEncoded := abiEncodeEmptyStringArrayArray(numPieces)
	sigEncoded := encodeBytes(sig)

	// Head: nonce (static) + 3 offsets for dynamic types
	headSize := 4 * 32
	keysOffset := headSize
	valsOffset := keysOffset + len(keysEncoded)
	sigOffset := valsOffset + len(valsEncoded)

	var buf []byte
	buf = append(buf, foc.EncodeBigInt(nonce)...)
	buf = append(buf, foc.EncodeBigInt(big.NewInt(int64(keysOffset)))...)
	buf = append(buf, foc.EncodeBigInt(big.NewInt(int64(valsOffset)))...)
	buf = append(buf, foc.EncodeBigInt(big.NewInt(int64(sigOffset)))...)
	buf = append(buf, keysEncoded...)
	buf = append(buf, valsEncoded...)
	buf = append(buf, sigEncoded...)
	return buf
}

// abiEncodeEmptyStringArrayArray encodes string[][] of length n where each inner string[] is empty.
func abiEncodeEmptyStringArrayArray(n int) []byte {
	// Layout: count | n offsets | n empty-arrays (each just length=0)
	var buf []byte
	buf = append(buf, foc.EncodeBigInt(big.NewInt(int64(n)))...)
	base := n * 32
	for i := 0; i < n; i++ {
		buf = append(buf, foc.EncodeBigInt(big.NewInt(int64(base+i*32)))...)
	}
	for i := 0; i < n; i++ {
		buf = append(buf, foc.EncodeBigInt(big.NewInt(0))...)
	}
	return buf
}

func encodeStringArray(strs []string) []byte {
	n := len(strs)
	var buf []byte
	buf = append(buf, foc.EncodeBigInt(big.NewInt(int64(n)))...)

	offsetBase := n * 32
	offsets := make([]int, n)
	currentOffset := offsetBase
	for i, s := range strs {
		offsets[i] = currentOffset
		currentOffset += 32 + padTo32(len(s))
	}
	for _, off := range offsets {
		buf = append(buf, foc.EncodeBigInt(big.NewInt(int64(off)))...)
	}

	for _, s := range strs {
		buf = append(buf, foc.EncodeBigInt(big.NewInt(int64(len(s))))...)
		data := []byte(s)
		buf = append(buf, data...)
		if pad := padTo32(len(data)) - len(data); pad > 0 {
			buf = append(buf, make([]byte, pad)...)
		}
	}

	return buf
}

func encodeBytes(data []byte) []byte {
	var buf []byte
	buf = append(buf, foc.EncodeBigInt(big.NewInt(int64(len(data))))...)
	buf = append(buf, data...)
	if pad := padTo32(len(data)) - len(data); pad > 0 {
		buf = append(buf, make([]byte, pad)...)
	}
	return buf
}

func padTo32(n int) int {
	if n == 0 {
		return 0
	}
	return ((n + 31) / 32) * 32
}

func buildCreateDataSetCalldata(fwssAddr []byte, extraData []byte) []byte {
	return foc.BuildCalldata(foc.SigCreateDataSet,
		foc.EncodeAddress(fwssAddr),
		foc.EncodeBigInt(big.NewInt(64)),
		encodeBytes(extraData),
	)
}
