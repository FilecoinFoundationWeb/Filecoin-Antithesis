package main

import (
	"log"
	"math/big"
	"time"

	"github.com/antithesishq/antithesis-sdk-go/assert"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/network"
	"github.com/filecoin-project/lotus/chain/types"
)

// ===========================================================================
// FIP-0115: base fee responds to mempool congestion (NV28+)
//
// Spec: under sustained high-premium mempool congestion the base fee should
// rise; once congestion clears, it should fall.
//
// Approach: only run after NV28 has activated everywhere, then sample the
// fee, flood the mempool for 60s, sample again. Both samples are under the
// FIP-0115 formula, so the comparison is clean.
//
// Env knobs (used as-is from old version; defaults below):
//   FIP0115_MSG_COUNT       (default 6000)  — target tx count for the flood
//   FIP0115_DURATION_SEC    (default 60)    — flood duration
//   FIP0115_PREMIUM_ATTO    (default 100000) — premium per tx (≥ spec floor)
// ===========================================================================

const (
	fip0115ActivationBuffer = 5    // epochs past NV28 boundary required before sampling
	fip0115MinSubmitted     = 500  // skip assertion if flood couldn't even land 500 txs
	fip0115RiseFloorPct     = 110  // peak >= pre * 1.10 to count as "rose" (10% rise)
)

func DoFIP0115BaseFeeResponse() {
	if partitionActive.Load() {
		return
	}
	initUpgradeState()
	nv28 := findBoundary("NV28")
	if nv28 == nil {
		return
	}

	// Require NV28 finalized + buffer past activation, on every node.
	snap := getFinalizedSnapshots()
	finalizedHeight, anchorKey := snapshotMinHeight(snap)
	if finalizedHeight < nv28.Epoch+fip0115ActivationBuffer {
		return
	}
	if !allNodesPostNV28(snap, anchorKey) {
		return
	}

	pre := sampleBaseFee()
	if pre == nil || pre.Sign() <= 0 {
		return
	}

	msgCount := envInt("FIP0115_MSG_COUNT", 6000)
	durationSec := envInt("FIP0115_DURATION_SEC", 60)
	premium := int64(envInt("FIP0115_PREMIUM_ATTO", 100_000))

	log.Printf("[fip0115] start: pre_basefee=%s target_msgs=%d duration=%ds accounts=%d",
		pre.String(), msgCount, durationSec, len(addrs))

	attempted, submitted := runFIP0115Flood(msgCount, durationSec, premium)
	if submitted < fip0115MinSubmitted {
		log.Printf("[fip0115] insufficient flood (submitted=%d/%d) — skipping assertion", submitted, attempted)
		return
	}

	// Sample peak immediately after flood. Base fee reacts within a couple
	// of blocks because each new block reads parent's gas usage; waiting any
	// longer risks the mempool draining and missing the peak.
	peak := sampleBaseFee()
	if peak == nil || peak.Sign() <= 0 {
		return
	}

	// Rise floor: peak >= pre * 1.10. Avoids declaring a single-atto increase
	// (natural variance) as evidence the fee responded.
	risen := new(big.Int).Mul(pre, big.NewInt(int64(fip0115RiseFloorPct)))
	risen.Quo(risen, big.NewInt(100))
	rose := peak.Cmp(risen) >= 0

	details := map[string]any{
		"pre_basefee":  pre.String(),
		"peak_basefee": peak.String(),
		"submitted":    submitted,
		"attempted":    attempted,
		"accounts":     len(addrs),
		"rise_floor":   risen.String(),
	}

	assert.Reachable("FIP-0115 flood completed", details)
	assert.Sometimes(rose, "FIP-0115: base fee rises under congestion (post-NV28)", details)

	if !rose {
		log.Printf("[fip0115] no rise: pre=%s peak=%s (need >= %s)",
			pre.String(), peak.String(), risen.String())
	} else {
		log.Printf("[fip0115] rose: pre=%s -> peak=%s", pre.String(), peak.String())
	}
}

// findBoundary returns the configured upgrade boundary by name, or nil.
func findBoundary(name string) *upgradeBoundary {
	for i := range upgradeBoundaries {
		if upgradeBoundaries[i].Name == name {
			return &upgradeBoundaries[i]
		}
	}
	return nil
}

// allNodesPostNV28 verifies every responding node reports network version 28+
// at the shared finalized anchor. Skips if any node disagrees — that means
// activation hasn't fully propagated and a flood now would conflate
// activation timing with congestion response.
func allNodesPostNV28(snap map[string]nodeSnapshot, anchorKey types.TipSetKey) bool {
	for name := range snap {
		if snap[name].err != nil {
			continue
		}
		nv, err := nodes[name].StateNetworkVersion(ctx, anchorKey)
		if err != nil {
			return false
		}
		if nv < network.Version28 {
			return false
		}
	}
	return true
}

// sampleBaseFee reads ParentBaseFee from the first responsive node's head.
// Sampled from chain head (not finalized) because base fee from N epochs
// ago doesn't reflect current mempool state — we want a fresh read.
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
