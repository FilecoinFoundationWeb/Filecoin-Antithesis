package main

import (
	"log"
	"math/big"
	"sync"
	"time"

	"github.com/antithesishq/antithesis-sdk-go/assert"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/types"
)

// ===========================================================================
// NV28-specific tests
//
// FIP-0115: base fee responds to mempool congestion.
//   Spec: submit >=6000 txs from >=2000 distinct accounts within 60s with
//   premium >=100k, starting immediately before upgrade activation. Verify
//   base fee rises as txs digest and falls after congestion clears.
//
// Env knobs (defaulted to spec; scale down if wallet pool is smaller):
//   FIP0115_MSG_COUNT       (default 6000)
//   FIP0115_DURATION_SEC    (default 60)
//   FIP0115_PREMIUM_ATTO    (default 100000)
//   FIP0115_PRE_LEAD_EPOCHS (default 2)
//
// The function is invoked repeatedly by the deck; a phase state machine
// advances the test across multiple invocations. The flood itself blocks
// the deck for ~DURATION_SEC because nonces has no mutex and deck actions
// are otherwise serial.
// ===========================================================================

func init() {
	RegisterFIPBoundaryFunc(doFIP0115BaseFeeResponse)
}

const (
	fip0115PhaseIdle    = 0
	fip0115PhaseFlooded = 1
	fip0115PhaseDuring  = 2
	fip0115PhaseDone    = 3
)

var (
	fip0115Mu        sync.Mutex
	fip0115Phase     int
	fip0115PreFee    *big.Int
	fip0115PeakFee   *big.Int
	fip0115Submitted int
	fip0115Attempted int
	fip0115Accounts  int
)

func doFIP0115BaseFeeResponse(currentHeight abi.ChainEpoch, b upgradeBoundary) {
	if b.Name != "NV28" {
		return
	}

	fip0115Mu.Lock()
	defer fip0115Mu.Unlock()

	if fip0115Phase == fip0115PhaseDone {
		return
	}

	msgCount := envInt("FIP0115_MSG_COUNT", 6000)
	durationSec := envInt("FIP0115_DURATION_SEC", 60)
	premium := int64(envInt("FIP0115_PREMIUM_ATTO", 100_000))
	preLead := abi.ChainEpoch(envInt("FIP0115_PRE_LEAD_EPOCHS", 2))

	switch fip0115Phase {
	case fip0115PhaseIdle:
		if currentHeight < b.Epoch-preLead || currentHeight >= b.Epoch {
			return
		}
		pre := sampleBaseFee()
		if pre == nil {
			return
		}
		fip0115PreFee = pre
		log.Printf("[fip0115] flood start: height=%d upgrade=%d pre_basefee=%s target_msgs=%d accts_available=%d",
			currentHeight, b.Epoch, pre.String(), msgCount, len(addrs))
		fip0115Attempted, fip0115Submitted = runFIP0115Flood(msgCount, durationSec, premium)
		fip0115Accounts = len(addrs)
		fip0115Phase = fip0115PhaseFlooded

	case fip0115PhaseFlooded:
		if currentHeight < b.Epoch+3 {
			return
		}
		peak := sampleBaseFee()
		if peak == nil {
			return
		}
		fip0115PeakFee = peak
		log.Printf("[fip0115] peak sample: height=%d basefee=%s (pre=%s)",
			currentHeight, peak.String(), fip0115PreFee.String())
		fip0115Phase = fip0115PhaseDuring

	case fip0115PhaseDuring:
		if currentHeight < b.Epoch+15 {
			return
		}
		post := sampleBaseFee()
		if post == nil {
			return
		}
		log.Printf("[fip0115] post sample: height=%d basefee=%s", currentHeight, post.String())

		rose := fip0115PeakFee.Cmp(fip0115PreFee) > 0
		fell := post.Cmp(fip0115PeakFee) < 0
		belowSpecAccounts := fip0115Accounts < 2000
		belowSpecMsgs := fip0115Submitted < 6000

		details := map[string]any{
			"boundary":         b.Name,
			"upgrade_epoch":    b.Epoch,
			"pre_basefee":      fip0115PreFee.String(),
			"peak_basefee":     fip0115PeakFee.String(),
			"post_basefee":     post.String(),
			"submitted":        fip0115Submitted,
			"attempted":        fip0115Attempted,
			"accounts":         fip0115Accounts,
			"below_spec_accts": belowSpecAccounts,
			"below_spec_msgs":  belowSpecMsgs,
			"sample_height":    int64(currentHeight),
		}

		assert.Sometimes(rose, "FIP-0115: base fee rises during congestion flood", details)
		assert.Sometimes(fell, "FIP-0115: base fee falls after congestion clears", details)

		if !rose {
			log.Printf("[fip0115] NO RISE: pre=%s peak=%s (submitted=%d/%d accts=%d)",
				fip0115PreFee.String(), fip0115PeakFee.String(),
				fip0115Submitted, fip0115Attempted, fip0115Accounts)
		}
		if !fell {
			log.Printf("[fip0115] NO FALL: peak=%s post=%s", fip0115PeakFee.String(), post.String())
		}

		fip0115Phase = fip0115PhaseDone
	}
}

// sampleBaseFee reads ParentBaseFee from the first responsive node's head.
func sampleBaseFee() *big.Int {
	for _, name := range nodeKeys {
		head, err := nodes[name].ChainHead(ctx)
		if err != nil || len(head.Blocks()) == 0 {
			continue
		}
		return new(big.Int).Set(head.Blocks()[0].ParentBaseFee.Int)
	}
	return nil
}

// runFIP0115Flood submits up to msgCount transfers across all deck wallets
// over roughly durationSec seconds, with the given premium. Returns
// (attempted, submitted). Blocks the calling goroutine.
func runFIP0115Flood(msgCount, durationSec int, premiumAtto int64) (int, int) {
	if len(addrs) == 0 {
		return 0, 0
	}

	feeCap := abi.NewTokenAmount(1_000_000_000_000)
	premium := abi.NewTokenAmount(premiumAtto)

	deadline := time.Now().Add(time.Duration(durationSec) * time.Second)
	start := time.Now()
	attempted, submitted := 0, 0

	for attempted < msgCount && time.Now().Before(deadline) {
		fromAddr := addrs[attempted%len(addrs)]
		fromKI := keystore[fromAddr]
		toAddr := addrs[(attempted+1)%len(addrs)]
		_, n := pickNode()

		msg := &types.Message{
			From:       fromAddr,
			To:         toAddr,
			Value:      abi.NewTokenAmount(1),
			Method:     0,
			GasLimit:   1_000_000,
			GasFeeCap:  feeCap,
			GasPremium: premium,
		}

		attempted++
		if pushMsg(n, msg, fromKI, "fip0115-flood") {
			submitted++
		}
	}

	log.Printf("[fip0115] flood done: submitted=%d attempted=%d in %.1fs",
		submitted, attempted, time.Since(start).Seconds())
	return attempted, submitted
}
