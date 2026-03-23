package main

import (
	"encoding/hex"
	"log"
	"math/big"
	"sync"

	"workload/internal/foc"

	"github.com/antithesishq/antithesis-sdk-go/assert"
	"github.com/antithesishq/antithesis-sdk-go/random"
	filbig "github.com/filecoin-project/go-state-types/big"
)

// ===========================================================================
// PDP Payment Accounting Vectors
//
// Exercises the dataset creation payment flow with a secondary client wallet
// that has a minimal USDFC balance. Verifies that payment rails correctly
// deduct fees from the client on dataset creation.
//
// The core invariant: after a confirmed dataset creation, the client's
// available USDFC in FilecoinPayV1 should decrease (fee extraction working).
// ===========================================================================

const griefUSDFCDeposit = 60000000000000000 // 0.06 USDFC (18 decimals)

// ---------------------------------------------------------------------------
// State
// ---------------------------------------------------------------------------

type griefState int

const (
	griefInit         griefState = iota
	griefFunded                  // USDFC transferred to secondary client
	griefActorCreated            // f4 actor exists on-chain (received native FIL)
	griefApproved                // secondary client approved FPV1 to spend USDFC
	griefDeposited               // secondary client deposited USDFC into FPV1
	griefOperatorOK              // secondary client approved FWSS as operator
	griefReady                   // ready to exercise payment flows
)

func (s griefState) String() string {
	switch s {
	case griefInit:
		return "Init"
	case griefFunded:
		return "Funded"
	case griefActorCreated:
		return "ActorCreated"
	case griefApproved:
		return "Approved"
	case griefDeposited:
		return "Deposited"
	case griefOperatorOK:
		return "OperatorApproved"
	case griefReady:
		return "Ready"
	default:
		return "Unknown"
	}
}

var (
	griefRT griefRuntime
	griefMu sync.Mutex
)

type griefRuntime struct {
	State     griefState
	ClientKey []byte   // 32-byte secp256k1 private key (secondary client)
	ClientEth []byte   // 20-byte ETH address
	InitFunds *big.Int // FPV1 funds snapshot after deposit
	DSCreated int
	LastFunds *big.Int

	// Extended state for additional probes
	LastOnChainDSID int       // most recent on-chain dataset ID
	LastClientDSID  *big.Int  // most recent clientDataSetId (for EIP-712)
	TermPending     bool      // terminateService called, waiting for delete
	DeletedCount    int
	SettledCount    int
	WithdrawCount   int
	UploadSessions  int
}

func griefSnap() griefRuntime {
	griefMu.Lock()
	defer griefMu.Unlock()
	return griefRT
}

// ---------------------------------------------------------------------------
// DoPDPGriefingProbe — single vector, two phases
// ---------------------------------------------------------------------------

func DoPDPGriefingProbe() {
	if focCfg == nil || focCfg.ClientKey == nil {
		return
	}

	// Wait for FOC lifecycle to be ready (contract addresses available)
	if _, ok := requireReady(); !ok {
		return
	}

	// Ensure required addresses are available
	if focCfg.USDFCAddr == nil || focCfg.FilPayAddr == nil || focCfg.FWSSAddr == nil {
		return
	}

	griefMu.Lock()
	currentState := griefRT.State
	griefMu.Unlock()

	switch currentState {
	case griefInit:
		doGriefInit()
	case griefFunded:
		doGriefCreateActor()
	case griefActorCreated:
		doGriefApprove()
	case griefApproved:
		doGriefDeposit()
	case griefDeposited:
		doGriefApproveOperator()
	case griefOperatorOK:
		doGriefArm()
	case griefReady:
		doGriefDispatch()
	}
}

// ---------------------------------------------------------------------------
// Setup Steps
// ---------------------------------------------------------------------------

// doGriefInit picks the secondary client wallet and transfers USDFC from the primary client.
func doGriefInit() {
	if len(addrs) < 2 {
		log.Printf("[sybil-fee-grief] not enough wallets in keystore")
		return
	}

	// Pick last wallet as dedicated secondary client and remove it from
	// the general pool so pickWallet() never selects it (avoids nonce collisions).
	griefMu.Lock()
	if griefRT.ClientKey == nil {
		addr := addrs[len(addrs)-1]
		ki := keystore[addr]
		griefRT.ClientKey = ki.PrivateKey
		griefRT.ClientEth = foc.DeriveEthAddr(ki.PrivateKey)
		addrs = addrs[:len(addrs)-1]
		log.Printf("[sybil-fee-grief] secondary client: filAddr=%s ethAddr=0x%x (removed from wallet pool)", addr, griefRT.ClientEth)
	}
	clientEth := griefRT.ClientEth
	griefMu.Unlock()

	if clientEth == nil {
		log.Printf("[sybil-fee-grief] failed to derive secondary client ETH address")
		return
	}

	node := focNode()

	// Transfer 0.06 USDFC from primary client to secondary client
	amount := big.NewInt(griefUSDFCDeposit)
	calldata := foc.BuildCalldata(foc.SigTransfer,
		foc.EncodeAddress(clientEth),
		foc.EncodeBigInt(amount),
	)

	log.Printf("[sybil-fee-grief] state=Init → funding secondary client with USDFC")
	ok := foc.SendEthTxConfirmed(ctx, node, focCfg.ClientKey, focCfg.USDFCAddr, calldata, "pdp-acct-fund")
	if !ok {
		log.Printf("[sybil-fee-grief] USDFC transfer failed, will retry")
		return
	}

	log.Printf("[sybil-fee-grief] secondary client funded")

	griefMu.Lock()
	griefRT.State = griefFunded
	griefMu.Unlock()
}

// doGriefCreateActor sends a small FIL transfer via EVM from the FOC client
// to the secondary client's ETH address, creating the f4 actor on-chain.
// Without this, EVM transactions from the secondary client fail with
// "actor not found". Uses the FOC client (which already has an f4 actor and
// FIL) to send the transaction.
func doGriefCreateActor() {
	s := griefSnap()
	node := focNode()

	log.Printf("[pdp-accounting] state=Funded → creating f4 actor via EVM transfer")

	// Send 0.001 FIL from FOC client to secondary client's ETH address.
	// This creates the f4 actor as a side effect of the EVM value transfer.
	oneMilliFIL := filbig.NewInt(1_000_000_000_000_000) // 0.001 FIL
	ok := foc.SendEthTxConfirmedWithValue(ctx, node, focCfg.ClientKey, s.ClientEth, oneMilliFIL, "pdp-acct-f4")
	if !ok {
		log.Printf("[pdp-accounting] f4 actor creation failed, will retry")
		return
	}

	log.Printf("[pdp-accounting] f4 actor created for ethAddr=0x%x", s.ClientEth)

	griefMu.Lock()
	griefRT.State = griefActorCreated
	griefMu.Unlock()
}

// doGriefApprove has the secondary client approve FPV1 to spend USDFC.
func doGriefApprove() {
	s := griefSnap()
	node := focNode()

	maxUint256 := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	calldata := foc.BuildCalldata(foc.SigApprove,
		foc.EncodeAddress(focCfg.FilPayAddr),
		foc.EncodeBigInt(maxUint256),
	)

	log.Printf("[sybil-fee-grief] state=ActorCreated → approving FPV1")
	ok := foc.SendEthTxConfirmed(ctx, node, s.ClientKey, focCfg.USDFCAddr, calldata, "pdp-acct-approve")
	if !ok {
		log.Printf("[sybil-fee-grief] approve failed, will retry")
		return
	}

	log.Printf("[sybil-fee-grief] FPV1 approved")

	griefMu.Lock()
	griefRT.State = griefApproved
	griefMu.Unlock()
}

// doGriefDeposit deposits USDFC into FPV1 for the secondary client.
func doGriefDeposit() {
	s := griefSnap()
	node := focNode()

	amount := big.NewInt(griefUSDFCDeposit)
	calldata := foc.BuildCalldata(foc.SigDeposit,
		foc.EncodeAddress(focCfg.USDFCAddr),
		foc.EncodeAddress(s.ClientEth),
		foc.EncodeBigInt(amount),
	)

	log.Printf("[sybil-fee-grief] state=Approved → depositing USDFC into FPV1")
	ok := foc.SendEthTxConfirmed(ctx, node, s.ClientKey, focCfg.FilPayAddr, calldata, "pdp-acct-deposit")
	if !ok {
		log.Printf("[sybil-fee-grief] deposit failed, will retry")
		return
	}

	funds := foc.ReadAccountFunds(ctx, node, focCfg.FilPayAddr, focCfg.USDFCAddr, s.ClientEth)
	log.Printf("[sybil-fee-grief] FPV1 funds after deposit: %s", funds)

	griefMu.Lock()
	griefRT.State = griefDeposited
	griefMu.Unlock()
}

// doGriefApproveOperator approves FWSS as operator for the secondary client on FPV1.
func doGriefApproveOperator() {
	s := griefSnap()
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

	log.Printf("[sybil-fee-grief] state=Deposited → approving FWSS as operator")
	ok := foc.SendEthTxConfirmed(ctx, node, s.ClientKey, focCfg.FilPayAddr, calldata, "pdp-acct-op")
	if !ok {
		log.Printf("[sybil-fee-grief] operator approval failed, will retry")
		return
	}

	log.Printf("[sybil-fee-grief] FWSS operator approved")

	griefMu.Lock()
	griefRT.State = griefOperatorOK
	griefMu.Unlock()
}

// doGriefArm snapshots initial funds and transitions to Ready.
func doGriefArm() {
	s := griefSnap()
	node := focNode()

	funds := foc.ReadAccountFunds(ctx, node, focCfg.FilPayAddr, focCfg.USDFCAddr, s.ClientEth)

	log.Printf("[sybil-fee-grief] state=OperatorApproved → ready. initialFunds=%s", funds)
	assert.Sometimes(true, "PDP secondary client setup completes", map[string]any{
		"initialFunds": funds.String(),
	})

	griefMu.Lock()
	griefRT.InitFunds = funds
	griefRT.State = griefReady
	griefMu.Unlock()
}

// ---------------------------------------------------------------------------
// Steady State — Probe Dispatcher
// ---------------------------------------------------------------------------

func doGriefDispatch() {
	type probe struct {
		name string
		fn   func()
	}
	probes := []probe{
		{"EmptyDatasetFee", probeEmptyDatasetFee},
		{"InsolvencyCreation", probeInsolvencyCreation},
		{"CrossPayerReplay", probeCrossPayerReplay},
		{"BurstCreation", probeBurstCreation},
	}
	pick := probes[rngIntn(len(probes))]
	log.Printf("[pdp-griefing] dispatching: %s", pick.name)
	pick.fn()
}

// ---------------------------------------------------------------------------
// Probe 1: Empty Dataset Fee Extraction
// ---------------------------------------------------------------------------

// probeEmptyDatasetFee creates an empty dataset via Curio HTTP and verifies
// that the client's USDFC balance in FPV1 decreases (fee extraction working).
func probeEmptyDatasetFee() {
	if !foc.PingCurio(ctx) {
		log.Printf("[sybil-fee-grief] curio not reachable, skipping")
		return
	}

	// Ensure SP key is loaded (needed for EIP-712 payee)
	if focCfg.SPKey == nil || focCfg.SPEthAddr == nil {
		focCfg.ReloadSPKey()
		if focCfg.SPKey == nil {
			log.Printf("[sybil-fee-grief] SP key not available, skipping")
			return
		}
	}

	s := griefSnap()
	node := focNode()

	// 1. Snapshot client FPV1 funds BEFORE
	fundsBefore := foc.ReadAccountFunds(ctx, node, focCfg.FilPayAddr, focCfg.USDFCAddr, s.ClientEth)
	if fundsBefore == nil || fundsBefore.Sign() == 0 {
		log.Printf("[sybil-fee-grief] client funds exhausted (%v), skipping", fundsBefore)
		return
	}

	// 2. Build dataset creation request (empty dataset, payer = secondary client)
	clientDataSetId := new(big.Int).SetUint64(random.GetRandom())
	metadataKeys := []string{"source"}
	metadataValues := []string{"antithesis-stress"}
	payee := focCfg.SPEthAddr

	sig, err := foc.SignEIP712CreateDataSet(
		s.ClientKey, focCfg.FWSSAddr,
		clientDataSetId, payee,
		metadataKeys, metadataValues,
	)
	if err != nil {
		log.Printf("[sybil-fee-grief] EIP-712 signing failed: %v", err)
		return
	}

	extraData := encodeCreateDataSetExtra(s.ClientEth, clientDataSetId, metadataKeys, metadataValues, sig)
	recordKeeper := "0x" + hex.EncodeToString(focCfg.FWSSAddr)

	// 3. Submit via Curio HTTP API
	log.Printf("[sybil-fee-grief] creating dataset: clientDataSetId=%s", clientDataSetId)
	txHash, err := foc.CreateDataSetHTTP(ctx, recordKeeper, hex.EncodeToString(extraData))
	if err != nil {
		log.Printf("[sybil-fee-grief] CreateDataSetHTTP failed: %v", err)
		return
	}

	// 4. Wait for on-chain confirmation
	onChainID, err := foc.WaitForDataSetCreation(ctx, txHash)
	if err != nil {
		log.Printf("[sybil-fee-grief] WaitForDataSetCreation failed: %v", err)
		return
	}

	// 5. Snapshot client FPV1 funds AFTER
	fundsAfter := foc.ReadAccountFunds(ctx, node, focCfg.FilPayAddr, focCfg.USDFCAddr, s.ClientEth)

	// 6. Invariant: payment rails should deduct fees from client on dataset creation
	fundsDecreased := fundsAfter.Cmp(fundsBefore) < 0
	delta := new(big.Int).Sub(fundsBefore, fundsAfter)

	assert.Sometimes(fundsDecreased,
		"dataset creation fee deducted from client USDFC",
		map[string]any{
			"fundsBefore":    fundsBefore.String(),
			"fundsAfter":     fundsAfter.String(),
			"delta":          delta.String(),
			"onChainID":      onChainID,
			"fundsDecreased": fundsDecreased,
		})

	griefMu.Lock()
	griefRT.DSCreated++
	griefRT.LastFunds = fundsAfter
	griefRT.LastOnChainDSID = onChainID
	griefRT.LastClientDSID = clientDataSetId
	created := griefRT.DSCreated
	griefMu.Unlock()

	log.Printf("[pdp-griefing] dataset created: onChainID=%d fundsBefore=%s fundsAfter=%s delta=%s decreased=%v total=%d",
		onChainID, fundsBefore, fundsAfter, delta, fundsDecreased, created)

	logGriefSPBalance()
}

// logGriefSPBalance logs the SP's FIL balance for observational purposes.
func logGriefSPBalance() {
	if focCfg.SPKey == nil {
		return
	}
	spFilAddr, err := foc.DeriveFilAddr(focCfg.SPKey)
	if err != nil {
		return
	}
	bal, err := focNode().WalletBalance(ctx, spFilAddr)
	if err != nil {
		return
	}

	s := griefSnap()
	log.Printf("[sybil-fee-grief] SP balance=%s datasetsCreated=%d", bal, s.DSCreated)
}

// ---------------------------------------------------------------------------
// Attack A3: Insolvency Creation
// ---------------------------------------------------------------------------

// probeInsolvencyCreation drains the secondary client's available USDFC, then
// tries to create a dataset. If creation succeeds with zero available funds,
// the SP gets a dataset with no payment guarantee — critical economic bug.
func probeInsolvencyCreation() {
	if !foc.PingCurio(ctx) {
		return
	}
	if focCfg.SPKey == nil || focCfg.SPEthAddr == nil {
		focCfg.ReloadSPKey()
		if focCfg.SPKey == nil {
			return
		}
	}

	s := griefSnap()
	node := focNode()

	// 1. Read current state
	funds := foc.ReadAccountFunds(ctx, node, focCfg.FilPayAddr, focCfg.USDFCAddr, s.ClientEth)
	lockup := foc.ReadAccountLockup(ctx, node, focCfg.FilPayAddr, focCfg.USDFCAddr, s.ClientEth)
	if funds == nil || lockup == nil {
		return
	}

	available := new(big.Int).Sub(funds, lockup)
	if available.Sign() <= 0 {
		// Already insolvent — try to create
		log.Printf("[pdp-griefing] client already insolvent (funds=%s lockup=%s), attempting create", funds, lockup)
	} else {
		// 2. Drain all available funds
		calldata := foc.BuildCalldata(foc.SigWithdraw,
			foc.EncodeAddress(focCfg.USDFCAddr),
			foc.EncodeBigInt(available),
		)

		log.Printf("[pdp-griefing] draining client: withdrawing %s available USDFC", available)
		ok := foc.SendEthTxConfirmed(ctx, node, s.ClientKey, focCfg.FilPayAddr, calldata, "pdp-griefing-drain")
		if !ok {
			log.Printf("[pdp-griefing] withdrawal failed, skipping insolvency test")
			return
		}

		// Confirm drained
		funds = foc.ReadAccountFunds(ctx, node, focCfg.FilPayAddr, focCfg.USDFCAddr, s.ClientEth)
		lockup = foc.ReadAccountLockup(ctx, node, focCfg.FilPayAddr, focCfg.USDFCAddr, s.ClientEth)
		available = new(big.Int).Sub(funds, lockup)
		log.Printf("[pdp-griefing] post-drain: funds=%s lockup=%s available=%s", funds, lockup, available)
	}

	// 3. Attempt dataset creation while insolvent
	clientDataSetId := new(big.Int).SetUint64(random.GetRandom())
	metadataKeys := []string{"source"}
	metadataValues := []string{"antithesis-stress"}

	sig, err := foc.SignEIP712CreateDataSet(
		s.ClientKey, focCfg.FWSSAddr,
		clientDataSetId, focCfg.SPEthAddr,
		metadataKeys, metadataValues,
	)
	if err != nil {
		log.Printf("[pdp-griefing] EIP-712 signing failed: %v", err)
		return
	}

	extraData := encodeCreateDataSetExtra(s.ClientEth, clientDataSetId, metadataKeys, metadataValues, sig)
	recordKeeper := "0x" + hex.EncodeToString(focCfg.FWSSAddr)

	log.Printf("[pdp-griefing] attempting dataset creation while insolvent (available=%s)", available)
	txHash, err := foc.CreateDataSetHTTP(ctx, recordKeeper, hex.EncodeToString(extraData))

	if err != nil {
		// HTTP-level rejection — Curio refused to submit the tx
		log.Printf("[pdp-griefing] insolvent create rejected at HTTP: %v", err)
		assert.Sometimes(true, "insolvent client dataset creation rejected", map[string]any{
			"available": available.String(),
			"error":     err.Error(),
		})
	} else {
		// Curio accepted — check if it actually landed on-chain
		onChainID, waitErr := foc.WaitForDataSetCreation(ctx, txHash)
		if waitErr != nil {
			// On-chain revert — correct behavior
			log.Printf("[pdp-griefing] insolvent create reverted on-chain: %v", waitErr)
			assert.Sometimes(true, "insolvent client dataset creation rejected", map[string]any{
				"available": available.String(),
			})
		} else {
			// CRITICAL: dataset created with insolvent client
			log.Printf("[pdp-griefing] CRITICAL: insolvent client created dataset! onChainID=%d available=%s", onChainID, available)
			assert.Sometimes(false, "insolvent client dataset creation rejected", map[string]any{
				"available":  available.String(),
				"onChainID":  onChainID,
				"fundsDrain": "client had zero available but creation succeeded",
			})
		}
	}

	// 4. Re-fund the secondary client for future probes
	refundAmount := big.NewInt(griefUSDFCDeposit)
	refundCalldata := foc.BuildCalldata(foc.SigTransfer,
		foc.EncodeAddress(s.ClientEth),
		foc.EncodeBigInt(refundAmount),
	)
	foc.SendEthTxConfirmed(ctx, node, focCfg.ClientKey, focCfg.USDFCAddr, refundCalldata, "pdp-griefing-refund")

	// Re-deposit into FPV1
	depositCalldata := foc.BuildCalldata(foc.SigDeposit,
		foc.EncodeAddress(focCfg.USDFCAddr),
		foc.EncodeAddress(s.ClientEth),
		foc.EncodeBigInt(refundAmount),
	)
	foc.SendEthTxConfirmed(ctx, node, s.ClientKey, focCfg.FilPayAddr, depositCalldata, "pdp-griefing-redeposit")

	log.Printf("[pdp-griefing] secondary client re-funded for future probes")
}

// ---------------------------------------------------------------------------
// Attack C2: Cross-Payer Signature Replay
// ---------------------------------------------------------------------------

// probeCrossPayerReplay signs a CreateDataSet EIP-712 message with the
// secondary client key but puts the PRIMARY client's address as the payer
// in the extraData. If the contract doesn't verify signer==payer, the primary
// client gets charged without consenting.
func probeCrossPayerReplay() {
	if !foc.PingCurio(ctx) {
		return
	}
	if focCfg.SPKey == nil || focCfg.SPEthAddr == nil {
		focCfg.ReloadSPKey()
		if focCfg.SPKey == nil {
			return
		}
	}

	s := griefSnap()
	node := focNode()

	// Read primary client's funds BEFORE
	primaryFundsBefore := foc.ReadAccountFunds(ctx, node, focCfg.FilPayAddr, focCfg.USDFCAddr, focCfg.ClientEthAddr)

	// Sign with SECONDARY client key (the attacker)
	clientDataSetId := new(big.Int).SetUint64(random.GetRandom())
	metadataKeys := []string{"source"}
	metadataValues := []string{"antithesis-stress"}

	sig, err := foc.SignEIP712CreateDataSet(
		s.ClientKey, focCfg.FWSSAddr, // signed by attacker
		clientDataSetId, focCfg.SPEthAddr,
		metadataKeys, metadataValues,
	)
	if err != nil {
		log.Printf("[pdp-griefing] EIP-712 signing failed: %v", err)
		return
	}

	// Build extraData with PRIMARY client as payer (not the signer!)
	extraData := encodeCreateDataSetExtra(focCfg.ClientEthAddr, clientDataSetId, metadataKeys, metadataValues, sig)
	recordKeeper := "0x" + hex.EncodeToString(focCfg.FWSSAddr)

	log.Printf("[pdp-griefing] attempting cross-payer replay: signer=secondary payer=primary")
	txHash, err := foc.CreateDataSetHTTP(ctx, recordKeeper, hex.EncodeToString(extraData))

	if err != nil {
		// Rejected at HTTP level
		log.Printf("[pdp-griefing] cross-payer replay rejected at HTTP: %v", err)
		assert.Sometimes(true, "cross-payer signature replay rejected", nil)
		return
	}

	// Check if it landed on-chain
	onChainID, waitErr := foc.WaitForDataSetCreation(ctx, txHash)
	if waitErr != nil {
		// On-chain revert — correct, signature didn't match payer
		log.Printf("[pdp-griefing] cross-payer replay reverted on-chain: %v", waitErr)
		assert.Sometimes(true, "cross-payer signature replay rejected", nil)
		return
	}

	// CRITICAL: creation succeeded — check if primary client was charged
	primaryFundsAfter := foc.ReadAccountFunds(ctx, node, focCfg.FilPayAddr, focCfg.USDFCAddr, focCfg.ClientEthAddr)
	primaryCharged := primaryFundsAfter.Cmp(primaryFundsBefore) < 0

	if primaryCharged {
		log.Printf("[pdp-griefing] CRITICAL: cross-payer replay succeeded! Primary client charged without signing. onChainID=%d", onChainID)
		assert.Sometimes(false, "cross-payer signature replay rejected", map[string]any{
			"onChainID":          onChainID,
			"primaryFundsBefore": primaryFundsBefore.String(),
			"primaryFundsAfter":  primaryFundsAfter.String(),
		})
	} else {
		// Creation succeeded but primary wasn't charged — maybe secondary was?
		log.Printf("[pdp-griefing] cross-payer replay: tx succeeded but primary not charged (onChainID=%d)", onChainID)
	}
}

// ---------------------------------------------------------------------------
// Attack D1: Burst Dataset Creation
// ---------------------------------------------------------------------------

// probeBurstCreation fires multiple dataset creation requests in rapid
// succession without waiting for confirmation. Tests whether Curio rate-limits
// and whether fees are correctly charged under load.
func probeBurstCreation() {
	if !foc.PingCurio(ctx) {
		return
	}
	if focCfg.SPKey == nil || focCfg.SPEthAddr == nil {
		focCfg.ReloadSPKey()
		if focCfg.SPKey == nil {
			return
		}
	}

	s := griefSnap()
	node := focNode()

	// Check we have funds for the burst
	fundsBefore := foc.ReadAccountFunds(ctx, node, focCfg.FilPayAddr, focCfg.USDFCAddr, s.ClientEth)
	if fundsBefore == nil || fundsBefore.Sign() == 0 {
		return
	}

	burstSize := 3 + rngIntn(3) // 3-5 requests
	accepted := 0
	recordKeeper := "0x" + hex.EncodeToString(focCfg.FWSSAddr)

	log.Printf("[pdp-griefing] starting burst creation: %d requests", burstSize)

	for i := 0; i < burstSize; i++ {
		clientDataSetId := new(big.Int).SetUint64(random.GetRandom())
		metadataKeys := []string{"source"}
		metadataValues := []string{"antithesis-stress"}

		sig, err := foc.SignEIP712CreateDataSet(
			s.ClientKey, focCfg.FWSSAddr,
			clientDataSetId, focCfg.SPEthAddr,
			metadataKeys, metadataValues,
		)
		if err != nil {
			continue
		}

		extraData := encodeCreateDataSetExtra(s.ClientEth, clientDataSetId, metadataKeys, metadataValues, sig)

		// Fire without waiting for confirmation
		_, err = foc.CreateDataSetHTTP(ctx, recordKeeper, hex.EncodeToString(extraData))
		if err != nil {
			log.Printf("[pdp-griefing] burst request %d/%d rejected: %v", i+1, burstSize, err)
		} else {
			accepted++
		}
	}

	// Check funds after burst
	fundsAfter := foc.ReadAccountFunds(ctx, node, focCfg.FilPayAddr, focCfg.USDFCAddr, s.ClientEth)
	delta := new(big.Int).Sub(fundsBefore, fundsAfter)

	log.Printf("[pdp-griefing] burst complete: accepted=%d/%d fundsBefore=%s fundsAfter=%s delta=%s",
		accepted, burstSize, fundsBefore, fundsAfter, delta)

	// If all requests accepted with no rate limiting, log it
	if accepted == burstSize {
		assert.Sometimes(true, "burst dataset creation accepted without rate limiting", map[string]any{
			"burstSize": burstSize,
			"accepted":  accepted,
		})
	}

	// Check fees were charged for accepted requests
	if accepted > 0 && delta.Sign() > 0 {
		assert.Sometimes(true, "burst creation charges fees correctly", map[string]any{
			"accepted":    accepted,
			"totalDelta":  delta.String(),
			"fundsBefore": fundsBefore.String(),
		})
	}

	griefMu.Lock()
	griefRT.DSCreated += accepted
	griefRT.LastFunds = fundsAfter
	griefMu.Unlock()
}

// ---------------------------------------------------------------------------
// Progress
// ---------------------------------------------------------------------------

func logGriefProgress() {
	s := griefSnap()
	if s.ClientEth == nil {
		return
	}
	log.Printf("[pdp-griefing] state=%s ds_created=%d initFunds=%v lastFunds=%v",
		s.State, s.DSCreated, s.InitFunds, s.LastFunds)
}
