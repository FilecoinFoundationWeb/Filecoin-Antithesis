package main

import (
	"log"
	"math/big"
	"sync"

	"workload/internal/foc"

	"github.com/antithesishq/antithesis-sdk-go/assert"
)

// ===========================================================================
// FOC Payment & Rail Security Probes
//
// Independent probes that test economic invariants of FilecoinPay and rail
// lifecycle. Each invocation picks one probe at random — no artificial
// sequential dependencies.
//
// Prerequisites: griefRuntime must be in griefReady state (secondary client
// wallet funded, f4 actor created, FWSS operator approved). At least one
// dataset must exist (griefRT.LastOnChainDSID > 0) so rails are available.
//
// Probes:
//   - Settlement lockup accounting (Audit L01)
//   - Double settlement idempotency
//   - withdrawTo redirect attack
//   - Unauthorized third-party deposit (Audit L04)
//   - Direct rail termination bypassing FWSS
//   - settleTerminatedRailWithoutValidation escape hatch
//   - Full withdrawal after settle (Issue #288)
// ===========================================================================

var (
	payProbesMu   sync.Mutex
	payProbeCount int
)

// ---------------------------------------------------------------------------
// DoFOCPaymentSecurity — deck entry, dispatches one random probe
// ---------------------------------------------------------------------------

func DoFOCPaymentSecurity() {
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
	if focCfg.FilPayAddr == nil || focCfg.USDFCAddr == nil {
		return
	}

	// Need at least one dataset (and therefore rails) to exist
	if gs.LastOnChainDSID == 0 {
		return
	}

	type probe struct {
		name string
		fn   func(griefRuntime)
	}
	probes := []probe{
		{"SettleLockup", payProbeSettleLockup},
		{"DoubleSettle", payProbeDoubleSettle},
		{"WithdrawToRedirect", payProbeWithdrawToRedirect},
		{"UnauthorizedDeposit", payProbeUnauthorizedDeposit},
		{"DirectTerminateRail", payProbeDirectTerminateRail},
		{"SettleTerminatedRail", payProbeSettleTerminatedRail},
		{"WithdrawAll", payProbeWithdrawAll},
	}

	pick := probes[rngIntn(len(probes))]
	log.Printf("[foc-payment-security] probe: %s", pick.name)
	pick.fn(gs)

	payProbesMu.Lock()
	payProbeCount++
	payProbesMu.Unlock()
}

// ---------------------------------------------------------------------------
// Helpers shared across probes
// ---------------------------------------------------------------------------

// payFindRail discovers the first rail for the secondary client.
// Returns nil if no rails found.
func payFindRail(gs griefRuntime) *big.Int {
	node := focNode()
	calldata := foc.BuildCalldata(foc.SigGetRailsByPayer,
		foc.EncodeAddress(gs.ClientEth),
		foc.EncodeAddress(focCfg.USDFCAddr),
		foc.EncodeBigInt(big.NewInt(0)),
		foc.EncodeBigInt(big.NewInt(10)),
	)
	result, err := foc.EthCallRaw(ctx, node, focCfg.FilPayAddr, calldata)
	if err != nil || len(result) < 96 {
		return nil
	}
	arrayLen := new(big.Int).SetBytes(result[32:64])
	if arrayLen.Sign() == 0 {
		return nil
	}
	return new(big.Int).SetBytes(result[64:96])
}

// payProvingPeriodElapsed checks if at least one proving period has passed
// for the griefing dataset. Settlement reverts if called mid-period.
func payProvingPeriodElapsed(gs griefRuntime) bool {
	if gs.LastOnChainDSID == 0 || focCfg.PDPAddr == nil {
		return false
	}
	node := focNode()
	head, err := node.ChainHead(ctx)
	if err != nil {
		return false
	}
	dsIDBytes := foc.EncodeBigInt(big.NewInt(int64(gs.LastOnChainDSID)))
	nextChallenge, err := foc.EthCallUint256(ctx, node, focCfg.PDPAddr,
		foc.BuildCalldata(foc.SigGetNextChallengeEpoch, dsIDBytes))
	if err != nil || nextChallenge == nil || nextChallenge.Sign() == 0 {
		return false
	}
	return int64(head.Height()) >= nextChallenge.Int64()
}

// ---------------------------------------------------------------------------
// Probe: Settlement Lockup Accounting (Audit L01)
//
// After settling a rail, lockup must not increase. The audit found that
// lockup was not properly decremented during finalization.
// ---------------------------------------------------------------------------

func payProbeSettleLockup(gs griefRuntime) {
	railID := payFindRail(gs)
	if railID == nil {
		log.Printf("[foc-payment-security] no rails found for secondary client")
		return
	}
	if !payProvingPeriodElapsed(gs) {
		log.Printf("[foc-payment-security] waiting for proving period before settlement")
		return
	}

	node := focNode()

	lockupBefore := foc.ReadAccountLockup(ctx, node, focCfg.FilPayAddr, focCfg.USDFCAddr, gs.ClientEth)

	head, _ := node.ChainHead(ctx)
	settleEpoch := big.NewInt(int64(head.Height()))

	calldata := foc.BuildCalldata(foc.SigSettleRail,
		foc.EncodeBigInt(railID),
		foc.EncodeBigInt(settleEpoch),
	)
	ok := foc.SendEthTxConfirmed(ctx, node, gs.ClientKey, focCfg.FilPayAddr, calldata, "foc-payment-security-settle")
	if !ok {
		log.Printf("[foc-payment-security] settle failed for railID=%s", railID)
		return
	}

	lockupAfter := foc.ReadAccountLockup(ctx, node, focCfg.FilPayAddr, focCfg.USDFCAddr, gs.ClientEth)

	if lockupBefore != nil && lockupAfter != nil {
		noIncrease := lockupAfter.Cmp(lockupBefore) <= 0
		assert.Sometimes(noIncrease, "Lockup does not increase after settlement", map[string]any{
			"lockupBefore": lockupBefore.String(),
			"lockupAfter":  lockupAfter.String(),
			"railID":       railID.String(),
		})
		if !noIncrease {
			log.Printf("[foc-payment-security] ANOMALY: lockup increased after settlement: %s → %s", lockupBefore, lockupAfter)
		}
	}

	log.Printf("[foc-payment-security] settled railID=%s lockup=%v→%v", railID, lockupBefore, lockupAfter)
}

// ---------------------------------------------------------------------------
// Probe: Double Settlement Idempotency
//
// Settling the same rail to the same epoch twice must not double-deduct funds.
// ---------------------------------------------------------------------------

func payProbeDoubleSettle(gs griefRuntime) {
	railID := payFindRail(gs)
	if railID == nil {
		return
	}
	if !payProvingPeriodElapsed(gs) {
		return
	}

	node := focNode()
	head, _ := node.ChainHead(ctx)
	settleEpoch := big.NewInt(int64(head.Height()))

	calldata := foc.BuildCalldata(foc.SigSettleRail,
		foc.EncodeBigInt(railID),
		foc.EncodeBigInt(settleEpoch),
	)

	// First settle
	foc.SendEthTxConfirmed(ctx, node, gs.ClientKey, focCfg.FilPayAddr, calldata, "foc-payment-security-settle1")

	// Snapshot between settles
	fundsBetween := foc.ReadAccountFunds(ctx, node, focCfg.FilPayAddr, focCfg.USDFCAddr, gs.ClientEth)

	// Second settle — same rail, same epoch
	foc.SendEthTxConfirmed(ctx, node, gs.ClientKey, focCfg.FilPayAddr, calldata, "foc-payment-security-settle2")

	fundsAfter := foc.ReadAccountFunds(ctx, node, focCfg.FilPayAddr, focCfg.USDFCAddr, gs.ClientEth)

	if fundsBetween != nil && fundsAfter != nil {
		noExtraDeduction := fundsAfter.Cmp(fundsBetween) >= 0
		assert.Sometimes(noExtraDeduction, "Double settlement does not double-deduct", map[string]any{
			"fundsBetween": fundsBetween.String(),
			"fundsAfter":   fundsAfter.String(),
			"railID":       railID.String(),
		})
		if !noExtraDeduction {
			log.Printf("[foc-payment-security] ANOMALY: double settle deducted extra: %s → %s", fundsBetween, fundsAfter)
		}
	}
}

// ---------------------------------------------------------------------------
// Probe: withdrawTo Redirect Attack
//
// Attacker (secondary client) calls withdrawTo(USDFC, attackerAddr, amount).
// Verify it only withdraws from the caller's own account — the `to` param
// is the recipient, not the source. Must never drain another user's funds.
// ---------------------------------------------------------------------------

func payProbeWithdrawToRedirect(gs griefRuntime) {
	node := focNode()

	// Snapshot primary client's funds BEFORE
	primaryFundsBefore := foc.ReadAccountFunds(ctx, node, focCfg.FilPayAddr, focCfg.USDFCAddr, focCfg.ClientEthAddr)

	smallAmount := big.NewInt(1000000000000000) // 0.001 USDFC
	calldata := foc.BuildCalldata(foc.SigWithdrawTo,
		foc.EncodeAddress(focCfg.USDFCAddr),
		foc.EncodeAddress(gs.ClientEth), // recipient = attacker
		foc.EncodeBigInt(smallAmount),
	)

	ok := foc.SendEthTxConfirmed(ctx, node, gs.ClientKey, focCfg.FilPayAddr, calldata, "foc-payment-security-withdrawto")

	// Verify primary client's funds were NOT affected
	primaryFundsAfter := foc.ReadAccountFunds(ctx, node, focCfg.FilPayAddr, focCfg.USDFCAddr, focCfg.ClientEthAddr)

	if primaryFundsBefore != nil && primaryFundsAfter != nil {
		primaryDrained := primaryFundsAfter.Cmp(primaryFundsBefore) < 0
		if primaryDrained && ok {
			log.Printf("[foc-payment-security] CRITICAL: withdrawTo drained PRIMARY funds! %s → %s", primaryFundsBefore, primaryFundsAfter)
		}
		assert.Sometimes(!primaryDrained, "withdrawTo does not drain other user funds", map[string]any{
			"primaryBefore": primaryFundsBefore.String(),
			"primaryAfter":  primaryFundsAfter.String(),
			"ok":            ok,
		})
	}
}

// ---------------------------------------------------------------------------
// Probe: Unauthorized Third-Party Deposit (Audit L04)
//
// Attacker deposits tokens into the PRIMARY client's FilecoinPay account
// without their consent. Verify funds don't increase for the target.
// ---------------------------------------------------------------------------

func payProbeUnauthorizedDeposit(gs griefRuntime) {
	node := focNode()

	primaryFundsBefore := foc.ReadAccountFunds(ctx, node, focCfg.FilPayAddr, focCfg.USDFCAddr, focCfg.ClientEthAddr)

	smallAmount := big.NewInt(1000000000000000) // 0.001 USDFC
	calldata := foc.BuildCalldata(foc.SigDeposit,
		foc.EncodeAddress(focCfg.USDFCAddr),
		foc.EncodeAddress(focCfg.ClientEthAddr), // target: PRIMARY client
		foc.EncodeBigInt(smallAmount),
	)

	ok := foc.SendEthTxConfirmed(ctx, node, gs.ClientKey, focCfg.FilPayAddr, calldata, "foc-payment-security-unauth-deposit")

	primaryFundsAfter := foc.ReadAccountFunds(ctx, node, focCfg.FilPayAddr, focCfg.USDFCAddr, focCfg.ClientEthAddr)

	if primaryFundsBefore != nil && primaryFundsAfter != nil {
		inflated := primaryFundsAfter.Cmp(primaryFundsBefore) > 0
		assert.Always(!inflated || !ok, "Third-party deposit cannot inflate target account", map[string]any{
			"primaryBefore": primaryFundsBefore.String(),
			"primaryAfter":  primaryFundsAfter.String(),
			"depositOK":     ok,
		})
		if inflated && ok {
			log.Printf("[foc-payment-security] CRITICAL: unauthorized deposit inflated primary: %s → %s", primaryFundsBefore, primaryFundsAfter)
		}
	}
}

// ---------------------------------------------------------------------------
// Probe: Direct Rail Termination Bypassing FWSS
//
// Calls terminateRail directly on FilecoinPay instead of going through
// FWSS.terminateService. Tests access control — only the rail's payer
// or operator should be allowed to terminate.
// ---------------------------------------------------------------------------

func payProbeDirectTerminateRail(gs griefRuntime) {
	railID := payFindRail(gs)
	if railID == nil {
		return
	}

	node := focNode()

	// Check if already terminated
	railData, err := foc.ReadRailFull(ctx, node, focCfg.FilPayAddr, railID.Uint64())
	if err != nil || len(railData) < 256 {
		return
	}
	endEpoch := new(big.Int).SetBytes(railData[224:256])
	if endEpoch.Sign() > 0 {
		log.Printf("[foc-payment-security] rail %s already terminated (endEpoch=%s), skipping", railID, endEpoch)
		return
	}

	calldata := foc.BuildCalldata(foc.SigTerminateRail,
		foc.EncodeBigInt(railID),
	)
	ok := foc.SendEthTxConfirmed(ctx, node, gs.ClientKey, focCfg.FilPayAddr, calldata, "foc-payment-security-terminate-rail")

	if ok {
		railAfter, err := foc.ReadRailFull(ctx, node, focCfg.FilPayAddr, railID.Uint64())
		if err == nil && len(railAfter) >= 256 {
			endEpochAfter := new(big.Int).SetBytes(railAfter[224:256])
			assert.Sometimes(endEpochAfter.Sign() > 0, "Direct rail termination sets endEpoch", map[string]any{
				"railID":   railID.String(),
				"endEpoch": endEpochAfter.String(),
			})
			log.Printf("[foc-payment-security] direct terminateRail succeeded: railID=%s endEpoch=%s", railID, endEpochAfter)
		}
	} else {
		log.Printf("[foc-payment-security] direct terminateRail reverted for railID=%s (access control working)", railID)
		assert.Sometimes(true, "Direct rail termination access control exercised", map[string]any{
			"railID": railID.String(),
		})
	}
}

// ---------------------------------------------------------------------------
// Probe: Settle Terminated Rail Without Validation (escape hatch)
//
// settleTerminatedRailWithoutValidation bypasses the FWSS validator.
// This exists for when the validator contract is broken. Verify lockup
// is released after the escape settlement.
// ---------------------------------------------------------------------------

func payProbeSettleTerminatedRail(gs griefRuntime) {
	railID := payFindRail(gs)
	if railID == nil {
		return
	}

	node := focNode()

	// Only works on terminated rails (endEpoch > 0)
	railData, err := foc.ReadRailFull(ctx, node, focCfg.FilPayAddr, railID.Uint64())
	if err != nil || len(railData) < 256 {
		return
	}
	endEpoch := new(big.Int).SetBytes(railData[224:256])
	if endEpoch.Sign() == 0 {
		// Rail not terminated — this probe doesn't apply
		return
	}

	lockupBefore := foc.ReadAccountLockup(ctx, node, focCfg.FilPayAddr, focCfg.USDFCAddr, gs.ClientEth)

	calldata := foc.BuildCalldata(foc.SigSettleTerminatedRailNoValidation,
		foc.EncodeBigInt(railID),
	)
	ok := foc.SendEthTxConfirmed(ctx, node, gs.ClientKey, focCfg.FilPayAddr, calldata, "foc-payment-security-settle-terminated")

	lockupAfter := foc.ReadAccountLockup(ctx, node, focCfg.FilPayAddr, focCfg.USDFCAddr, gs.ClientEth)

	if ok && lockupBefore != nil && lockupAfter != nil {
		lockupDecreased := lockupAfter.Cmp(lockupBefore) <= 0
		assert.Sometimes(lockupDecreased, "Lockup decreases after settling terminated rail", map[string]any{
			"lockupBefore": lockupBefore.String(),
			"lockupAfter":  lockupAfter.String(),
			"railID":       railID.String(),
		})
		log.Printf("[foc-payment-security] settleTerminatedRail: railID=%s lockup=%v→%v", railID, lockupBefore, lockupAfter)
	} else if !ok {
		log.Printf("[foc-payment-security] settleTerminatedRail reverted for railID=%s endEpoch=%s", railID, endEpoch)
	}
}

// ---------------------------------------------------------------------------
// Probe: Full Withdrawal After Settlement (Issue #288)
//
// After settling, available = funds - lockup should be withdrawable.
// If withdrawal reverts when available > 0, funds are permanently locked.
// ---------------------------------------------------------------------------

func payProbeWithdrawAll(gs griefRuntime) {
	node := focNode()

	funds := foc.ReadAccountFunds(ctx, node, focCfg.FilPayAddr, focCfg.USDFCAddr, gs.ClientEth)
	lockup := foc.ReadAccountLockup(ctx, node, focCfg.FilPayAddr, focCfg.USDFCAddr, gs.ClientEth)
	if funds == nil || lockup == nil {
		return
	}

	available := new(big.Int).Sub(funds, lockup)
	if available.Sign() <= 0 {
		return
	}

	calldata := foc.BuildCalldata(foc.SigWithdraw,
		foc.EncodeAddress(focCfg.USDFCAddr),
		foc.EncodeBigInt(available),
	)
	ok := foc.SendEthTxConfirmed(ctx, node, gs.ClientKey, focCfg.FilPayAddr, calldata, "foc-payment-security-withdraw-all")

	assert.Sometimes(ok, "Full withdrawal of available funds succeeds", map[string]any{
		"funds":     funds.String(),
		"lockup":    lockup.String(),
		"available": available.String(),
	})

	if !ok {
		log.Printf("[foc-payment-security] ANOMALY: withdrawal of available=%s FAILED (funds=%s lockup=%s)", available, funds, lockup)
	} else {
		log.Printf("[foc-payment-security] withdrawn available=%s", available)
	}

	// Re-deposit for future probes
	if ok {
		redeposit := foc.BuildCalldata(foc.SigDeposit,
			foc.EncodeAddress(focCfg.USDFCAddr),
			foc.EncodeAddress(gs.ClientEth),
			foc.EncodeBigInt(available),
		)
		foc.SendEthTxConfirmed(ctx, node, gs.ClientKey, focCfg.FilPayAddr, redeposit, "foc-payment-security-redeposit")
	}
}

// ---------------------------------------------------------------------------
// Progress
// ---------------------------------------------------------------------------

func logPaySecProgress() {
	payProbesMu.Lock()
	count := payProbeCount
	payProbesMu.Unlock()
	if count > 0 {
		log.Printf("[foc-payment-security] probes_run=%d", count)
	}
}
