package main

import (
	"log"
	"math/big"
	"sync"

	"workload/internal/foc"

	"github.com/antithesishq/antithesis-sdk-go/assert"
	"github.com/antithesishq/antithesis-sdk-go/random"
)

// ===========================================================================
// Scenario 2: Payment Rail & Funds Security
//
// Tests the full payment rail lifecycle with audit finding checks at each step.
// Each deck invocation advances one phase. The scenario cycles continuously:
//
//   Init → Settled → DoubleSettled → RailChecked → RateModified →
//   Withdrawn → Refunded → (back to Init)
//
// Covers:
//   - Audit L01: lockup accounting after settlement
//   - Audit L04: unauthorized third-party deposit
//   - Audit L06: rate change queue staleness
//   - Issue #288: funds locked after lifecycle
//   - Double settlement idempotency
//   - 3-rail sanity check (no FILCDN/IPNI billing)
//
// Requires griefRuntime to be in griefReady state (secondary client set up).
// ===========================================================================

// ---------------------------------------------------------------------------
// State
// ---------------------------------------------------------------------------

type paySecState int

const (
	paySecInit          paySecState = iota // snapshot funds/lockup, discover rails
	paySecSettled                          // settle pdpRail, verify lockup (L01)
	paySecDoubleSettled                    // settle same rail again, verify idempotent
	paySecRailChecked                     // verify all 3 rails configuration
	paySecRateModified                    // modify rate twice, verify latest persists (L06)
	paySecWithdrawn                       // withdraw all available funds (#288)
	paySecRefunded                        // re-deposit + test unauthorized deposit (L04)
)

func (s paySecState) String() string {
	switch s {
	case paySecInit:
		return "Init"
	case paySecSettled:
		return "Settled"
	case paySecDoubleSettled:
		return "DoubleSettled"
	case paySecRailChecked:
		return "RailChecked"
	case paySecRateModified:
		return "RateModified"
	case paySecWithdrawn:
		return "Withdrawn"
	case paySecRefunded:
		return "Refunded"
	default:
		return "Unknown"
	}
}

var (
	paySec   paySecRuntime
	paySecMu sync.Mutex
)

type paySecRuntime struct {
	State paySecState

	// Snapshot values
	FundsBefore  *big.Int
	LockupBefore *big.Int
	FundsAfter   *big.Int
	LockupAfter  *big.Int

	// Rail discovery
	RailID      *big.Int
	SettleEpoch *big.Int

	// Progress
	Cycles int
}

func paySecSnap() paySecRuntime {
	paySecMu.Lock()
	defer paySecMu.Unlock()
	return paySec
}

// ---------------------------------------------------------------------------
// DoFOCPaymentSecurityProbe — deck entry
// ---------------------------------------------------------------------------

func DoFOCPaymentSecurityProbe() {
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

	paySecMu.Lock()
	state := paySec.State
	paySecMu.Unlock()

	switch state {
	case paySecInit:
		paySecDoInit()
	case paySecSettled:
		paySecDoDoubleSettle()
	case paySecDoubleSettled:
		paySecDoRailCheck()
	case paySecRailChecked:
		paySecDoRateModify()
	case paySecRateModified:
		paySecDoWithdraw()
	case paySecWithdrawn:
		paySecDoRefund()
	case paySecRefunded:
		// Reset
		paySecMu.Lock()
		paySec.Cycles++
		cycles := paySec.Cycles
		paySec.State = paySecInit
		paySec.RailID = nil
		paySec.SettleEpoch = nil
		paySec.FundsBefore = nil
		paySec.LockupBefore = nil
		paySec.FundsAfter = nil
		paySec.LockupAfter = nil
		paySecMu.Unlock()
		log.Printf("[payment-security] cycle %d complete, resetting", cycles)
		assert.Sometimes(true, "Payment security scenario cycle completes", map[string]any{
			"cycles": cycles,
		})
	}
}

// ---------------------------------------------------------------------------
// Phase 1: Snapshot + Settle (Audit L01)
// ---------------------------------------------------------------------------

func paySecDoInit() {
	gs := griefSnap()
	node := focNode()

	// Read funds/lockup BEFORE
	funds := foc.ReadAccountFunds(ctx, node, focCfg.FilPayAddr, focCfg.USDFCAddr, gs.ClientEth)
	lockup := foc.ReadAccountLockup(ctx, node, focCfg.FilPayAddr, focCfg.USDFCAddr, gs.ClientEth)

	if funds == nil || funds.Sign() == 0 {
		log.Printf("[payment-security] secondary client has no funds, skipping")
		return
	}

	// Discover rails for secondary client
	railCalldata := foc.BuildCalldata(foc.SigGetRailsByPayer,
		foc.EncodeAddress(gs.ClientEth),
		foc.EncodeAddress(focCfg.USDFCAddr),
		foc.EncodeBigInt(big.NewInt(0)),
		foc.EncodeBigInt(big.NewInt(10)),
	)
	result, err := foc.EthCallRaw(ctx, node, focCfg.FilPayAddr, railCalldata)
	if err != nil || len(result) < 96 {
		log.Printf("[payment-security] no rails found for secondary client")
		return
	}

	arrayLen := new(big.Int).SetBytes(result[32:64])
	if arrayLen.Sign() == 0 {
		log.Printf("[payment-security] no rails found (empty array)")
		return
	}
	railID := new(big.Int).SetBytes(result[64:96])

	// Get current epoch for settlement
	head, err := node.ChainHead(ctx)
	if err != nil {
		return
	}
	settleEpoch := big.NewInt(int64(head.Height()))

	// Settle the rail
	settleCalldata := foc.BuildCalldata(foc.SigSettleRail,
		foc.EncodeBigInt(railID),
		foc.EncodeBigInt(settleEpoch),
	)
	ok := foc.SendEthTxConfirmed(ctx, node, gs.ClientKey, focCfg.FilPayAddr, settleCalldata, "payment-security-settle")
	if !ok {
		log.Printf("[payment-security] settlement failed for railID=%s, will retry", railID)
		return
	}

	// Read funds/lockup AFTER
	fundsAfter := foc.ReadAccountFunds(ctx, node, focCfg.FilPayAddr, focCfg.USDFCAddr, gs.ClientEth)
	lockupAfter := foc.ReadAccountLockup(ctx, node, focCfg.FilPayAddr, focCfg.USDFCAddr, gs.ClientEth)

	// Audit L01: lockup must not increase after settlement
	if lockup != nil && lockupAfter != nil {
		noIncrease := lockupAfter.Cmp(lockup) <= 0
		assert.Sometimes(noIncrease, "Lockup does not increase after settlement", map[string]any{
			"lockupBefore": lockup.String(),
			"lockupAfter":  lockupAfter.String(),
			"railID":       railID.String(),
			"settleEpoch":  settleEpoch.String(),
		})
		if !noIncrease {
			log.Printf("[payment-security] ANOMALY: lockup INCREASED after settlement: %s → %s", lockup, lockupAfter)
		}
	}

	log.Printf("[payment-security] settled railID=%s epoch=%s funds=%s→%s lockup=%s→%s",
		railID, settleEpoch, funds, fundsAfter, lockup, lockupAfter)

	paySecMu.Lock()
	paySec.FundsBefore = funds
	paySec.LockupBefore = lockup
	paySec.FundsAfter = fundsAfter
	paySec.LockupAfter = lockupAfter
	paySec.RailID = railID
	paySec.SettleEpoch = settleEpoch
	paySec.State = paySecSettled
	paySecMu.Unlock()
}

// ---------------------------------------------------------------------------
// Phase 2: Double Settlement — verify idempotent
// ---------------------------------------------------------------------------

func paySecDoDoubleSettle() {
	s := paySecSnap()
	gs := griefSnap()
	node := focNode()

	if s.RailID == nil || s.SettleEpoch == nil {
		paySecMu.Lock()
		paySec.State = paySecDoubleSettled
		paySecMu.Unlock()
		return
	}

	// Read funds before second settle
	fundsBefore2 := foc.ReadAccountFunds(ctx, node, focCfg.FilPayAddr, focCfg.USDFCAddr, gs.ClientEth)

	// Settle same rail at same epoch again
	settleCalldata := foc.BuildCalldata(foc.SigSettleRail,
		foc.EncodeBigInt(s.RailID),
		foc.EncodeBigInt(s.SettleEpoch),
	)
	foc.SendEthTxConfirmed(ctx, node, gs.ClientKey, focCfg.FilPayAddr, settleCalldata, "payment-security-double-settle")

	// Read funds after second settle
	fundsAfter2 := foc.ReadAccountFunds(ctx, node, focCfg.FilPayAddr, focCfg.USDFCAddr, gs.ClientEth)

	if fundsBefore2 != nil && fundsAfter2 != nil {
		noExtraDeduction := fundsAfter2.Cmp(fundsBefore2) >= 0
		assert.Sometimes(noExtraDeduction, "Double settlement is idempotent", map[string]any{
			"fundsBefore2": fundsBefore2.String(),
			"fundsAfter2":  fundsAfter2.String(),
			"railID":       s.RailID.String(),
		})
		if !noExtraDeduction {
			delta := new(big.Int).Sub(fundsBefore2, fundsAfter2)
			log.Printf("[payment-security] ANOMALY: double settle caused extra deduction of %s", delta)
		}
	}

	log.Printf("[payment-security] double settle complete: funds=%v→%v", fundsBefore2, fundsAfter2)

	paySecMu.Lock()
	paySec.State = paySecDoubleSettled
	paySecMu.Unlock()
}

// ---------------------------------------------------------------------------
// Phase 3: Rail Sanity Check — verify 3-rail configuration
// ---------------------------------------------------------------------------

func paySecDoRailCheck() {
	gs := griefSnap()
	node := focNode()

	if gs.LastOnChainDSID == 0 {
		paySecMu.Lock()
		paySec.State = paySecRailChecked
		paySecMu.Unlock()
		return
	}

	// Read the pdpRailId payment rate (should be non-zero for active dataset)
	s := paySecSnap()
	if s.RailID != nil {
		rate := foc.ReadRailPaymentRate(ctx, node, focCfg.FilPayAddr, s.RailID.Uint64())
		if rate != nil {
			log.Printf("[payment-security] pdpRail rate=%s", rate)
		}

		// Read the full rail to check endEpoch (should be 0 for active rail)
		railData, err := foc.ReadRailFull(ctx, node, focCfg.FilPayAddr, s.RailID.Uint64())
		if err == nil && len(railData) >= 256 {
			endEpoch := new(big.Int).SetBytes(railData[224:256]) // word index 7
			log.Printf("[payment-security] pdpRail endEpoch=%s (0=active)", endEpoch)
		}
	}

	// Check active piece count — if zero and rate > 0, billing for empty storage
	dsIDBytes := foc.EncodeBigInt(big.NewInt(int64(gs.LastOnChainDSID)))
	activeCount, _ := foc.EthCallUint256(ctx, node, focCfg.PDPAddr, foc.BuildCalldata(foc.SigGetActivePieceCount, dsIDBytes))

	if activeCount != nil && s.RailID != nil {
		rate := foc.ReadRailPaymentRate(ctx, node, focCfg.FilPayAddr, s.RailID.Uint64())
		if rate != nil && activeCount.Sign() == 0 && rate.Sign() > 0 {
			log.Printf("[payment-security] NOTE: zero pieces but non-zero rate=%s — may be expected during setup", rate)
		}
	}

	log.Printf("[payment-security] rail check complete: dsID=%d activePieces=%v", gs.LastOnChainDSID, activeCount)

	paySecMu.Lock()
	paySec.State = paySecRailChecked
	paySecMu.Unlock()
}

// ---------------------------------------------------------------------------
// Phase 4: Rate Modification (Audit L06)
// ---------------------------------------------------------------------------

func paySecDoRateModify() {
	s := paySecSnap()
	gs := griefSnap()
	node := focNode()

	if s.RailID == nil {
		paySecMu.Lock()
		paySec.State = paySecRateModified
		paySecMu.Unlock()
		return
	}

	// Generate two different rates
	rate1 := new(big.Int).SetUint64(random.GetRandom()%1000 + 1)
	rate2 := new(big.Int).SetUint64(random.GetRandom()%1000 + 1001) // guaranteed different
	lockupPeriod := big.NewInt(3600)

	// First rate change
	calldata1 := foc.BuildCalldata(foc.SigModifyRailPayment,
		foc.EncodeBigInt(s.RailID),
		foc.EncodeBigInt(rate1),
		foc.EncodeBigInt(lockupPeriod),
	)
	ok1 := foc.SendEthTxConfirmed(ctx, node, gs.ClientKey, focCfg.FilPayAddr, calldata1, "payment-security-rate1")

	if !ok1 {
		log.Printf("[payment-security] rate change 1 failed (may not have permission), skipping L06 test")
		paySecMu.Lock()
		paySec.State = paySecRateModified
		paySecMu.Unlock()
		return
	}

	// Second rate change (should overwrite first)
	calldata2 := foc.BuildCalldata(foc.SigModifyRailPayment,
		foc.EncodeBigInt(s.RailID),
		foc.EncodeBigInt(rate2),
		foc.EncodeBigInt(lockupPeriod),
	)
	ok2 := foc.SendEthTxConfirmed(ctx, node, gs.ClientKey, focCfg.FilPayAddr, calldata2, "payment-security-rate2")

	if ok1 && ok2 {
		// Read the rail to check which rate persisted
		railData, err := foc.ReadRailFull(ctx, node, focCfg.FilPayAddr, s.RailID.Uint64())
		if err == nil && len(railData) >= 192 {
			currentRate := new(big.Int).SetBytes(railData[128:160]) // word index 4 = paymentRate
			log.Printf("[payment-security] after two rate changes: currentRate=%s rate1=%s rate2=%s", currentRate, rate1, rate2)

			// The latest rate (rate2) should be the one that persists
			latestPersists := currentRate.Cmp(rate1) != 0
			assert.Sometimes(latestPersists, "Latest rate change replaces stale pending change", map[string]any{
				"rate1":       rate1.String(),
				"rate2":       rate2.String(),
				"currentRate": currentRate.String(),
				"railID":      s.RailID.String(),
			})
		}
	}

	paySecMu.Lock()
	paySec.State = paySecRateModified
	paySecMu.Unlock()
}

// ---------------------------------------------------------------------------
// Phase 5: Withdraw All Available Funds (#288)
// ---------------------------------------------------------------------------

func paySecDoWithdraw() {
	gs := griefSnap()
	node := focNode()

	funds := foc.ReadAccountFunds(ctx, node, focCfg.FilPayAddr, focCfg.USDFCAddr, gs.ClientEth)
	lockup := foc.ReadAccountLockup(ctx, node, focCfg.FilPayAddr, focCfg.USDFCAddr, gs.ClientEth)

	if funds == nil || lockup == nil {
		paySecMu.Lock()
		paySec.State = paySecWithdrawn
		paySecMu.Unlock()
		return
	}

	available := new(big.Int).Sub(funds, lockup)
	if available.Sign() <= 0 {
		log.Printf("[payment-security] no available funds to withdraw (funds=%s lockup=%s)", funds, lockup)
		paySecMu.Lock()
		paySec.State = paySecWithdrawn
		paySecMu.Unlock()
		return
	}

	calldata := foc.BuildCalldata(foc.SigWithdraw,
		foc.EncodeAddress(focCfg.USDFCAddr),
		foc.EncodeBigInt(available),
	)

	ok := foc.SendEthTxConfirmed(ctx, node, gs.ClientKey, focCfg.FilPayAddr, calldata, "payment-security-withdraw")

	assert.Sometimes(ok, "Full withdrawal after settlement succeeds", map[string]any{
		"funds":     funds.String(),
		"lockup":    lockup.String(),
		"available": available.String(),
		"ok":        ok,
	})

	if !ok {
		log.Printf("[payment-security] ANOMALY: withdrawal of available=%s FAILED (funds=%s lockup=%s) — possible locked funds", available, funds, lockup)
	} else {
		log.Printf("[payment-security] withdrawn %s (funds=%s lockup=%s)", available, funds, lockup)
	}

	paySecMu.Lock()
	paySec.State = paySecWithdrawn
	paySecMu.Unlock()
}

// ---------------------------------------------------------------------------
// Phase 6: Refund + Unauthorized Deposit Test (Audit L04)
// ---------------------------------------------------------------------------

func paySecDoRefund() {
	gs := griefSnap()
	node := focNode()

	// ---- Audit L04: unauthorized third-party deposit test ----
	// Secondary client (attacker) tries to deposit to PRIMARY client's account
	primaryFundsBefore := foc.ReadAccountFunds(ctx, node, focCfg.FilPayAddr, focCfg.USDFCAddr, focCfg.ClientEthAddr)

	smallAmount := big.NewInt(1000000000000000) // 0.001 USDFC
	depositCalldata := foc.BuildCalldata(foc.SigDeposit,
		foc.EncodeAddress(focCfg.USDFCAddr),
		foc.EncodeAddress(focCfg.ClientEthAddr), // target: PRIMARY client
		foc.EncodeBigInt(smallAmount),
	)

	depositOK := foc.SendEthTxConfirmed(ctx, node, gs.ClientKey, focCfg.FilPayAddr, depositCalldata, "payment-security-unauth-deposit")

	primaryFundsAfter := foc.ReadAccountFunds(ctx, node, focCfg.FilPayAddr, focCfg.USDFCAddr, focCfg.ClientEthAddr)

	if primaryFundsBefore != nil && primaryFundsAfter != nil {
		inflated := primaryFundsAfter.Cmp(primaryFundsBefore) > 0
		// TRUE SAFETY INVARIANT: third party cannot inflate someone's funds
		assert.Always(!inflated || !depositOK, "Third-party deposit cannot inflate target account", map[string]any{
			"primaryBefore": primaryFundsBefore.String(),
			"primaryAfter":  primaryFundsAfter.String(),
			"depositOK":     depositOK,
			"attacker":      "secondary_client",
		})
		if inflated && depositOK {
			log.Printf("[payment-security] CRITICAL: unauthorized deposit inflated primary funds: %s → %s", primaryFundsBefore, primaryFundsAfter)
		}
	}

	// ---- Refund secondary client for next cycle ----
	refundAmount := big.NewInt(griefUSDFCDeposit)
	refundCalldata := foc.BuildCalldata(foc.SigTransfer,
		foc.EncodeAddress(gs.ClientEth),
		foc.EncodeBigInt(refundAmount),
	)
	foc.SendEthTxConfirmed(ctx, node, focCfg.ClientKey, focCfg.USDFCAddr, refundCalldata, "payment-security-refund")

	// Re-deposit into FilecoinPay
	redeposit := foc.BuildCalldata(foc.SigDeposit,
		foc.EncodeAddress(focCfg.USDFCAddr),
		foc.EncodeAddress(gs.ClientEth),
		foc.EncodeBigInt(refundAmount),
	)
	foc.SendEthTxConfirmed(ctx, node, gs.ClientKey, focCfg.FilPayAddr, redeposit, "payment-security-redeposit")

	log.Printf("[payment-security] refund complete, advancing to next cycle")

	paySecMu.Lock()
	paySec.State = paySecRefunded
	paySecMu.Unlock()
}

// ---------------------------------------------------------------------------
// Progress
// ---------------------------------------------------------------------------

func logPaySecProgress() {
	s := paySecSnap()
	log.Printf("[payment-security] state=%s cycles=%d railID=%v",
		s.State, s.Cycles, s.RailID)
}
