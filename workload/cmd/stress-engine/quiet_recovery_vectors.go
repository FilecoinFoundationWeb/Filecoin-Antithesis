package main

import (
	"log"
	"os"
	"os/exec"
	"time"

	"github.com/antithesishq/antithesis-sdk-go/assert"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/types"
)

// ===========================================================================
// Quiet Recovery Vector
//
// Pauses Antithesis fault injection for a configurable duration, then verifies
// that the Filecoin network self-heals: chain advances, nodes converge, and
// all nodes agree on the tipset at finalized height.
//
// Gated by QUIET_RECOVERY_ENABLED=1 (off by default — pausing faults is
// disruptive to the entire devnet). Enable via the notebook or GH Action toggle.
//
// Requires the ANTITHESIS_STOP_FAULTS binary injected by the Antithesis runtime.
// ===========================================================================

const (
	quietDuration       = "45" // seconds to pause faults (string for exec arg)
	quietStabilizeSecs  = 15   // seconds to wait for gossip/reconnection after faults pause
	quietDriftThreshold = 3    // max block drift to consider nodes "converged"
	quietFinalityBuffer = 10   // epochs below min post-recovery height to check tipset agreement
)

var (
	quietRecoveryRemaining  = -1        // -1 = not yet initialized; 0–5 once set
	quietRecoveryEarliestRun time.Time  // earliest wall-clock time the next execution is allowed
)

// DoQuietRecovery requests a fault-free window and verifies chain self-healing.
func DoQuietRecovery() {
	if os.Getenv("QUIET_RECOVERY_ENABLED") != "1" {
		return
	}

	// Skip if a workload-driven partition is active (n-split lifecycle or
	// reorg-chaos). ANTITHESIS_STOP_FAULTS only pauses the fault injector,
	// not RPC-applied NetBlockAdd/NetDisconnect from other vectors, so the
	// drift assertion would falsely fail on the still-isolated nodes.
	if partitionActive.Load() {
		debugLog("[quiet-recovery] skipping — partition already active")
		return
	}

	// One-time init: pick 0–5 total executions, delay first by 0–3 min
	if quietRecoveryRemaining == -1 {
		quietRecoveryRemaining = rngIntn(6)
		quietRecoveryEarliestRun = time.Now().Add(time.Duration(rngIntn(180)) * time.Second)
		log.Printf("[quiet-recovery] will fire at most %d times, first eligible at %v", quietRecoveryRemaining, quietRecoveryEarliestRun)
	}

	if quietRecoveryRemaining <= 0 {
		return
	}

	if time.Now().Before(quietRecoveryEarliestRun) {
		return
	}

	quietRecoveryRemaining--
	quietRecoveryEarliestRun = time.Now().Add(time.Duration(120+rngIntn(300)) * time.Second)
	log.Printf("[quiet-recovery] firing (remaining=%d, next eligible at %v)", quietRecoveryRemaining, quietRecoveryEarliestRun)

	stopBin := os.Getenv("ANTITHESIS_STOP_FAULTS")
	if stopBin == "" {
		debugLog("[quiet-recovery] ANTITHESIS_STOP_FAULTS not set, skipping")
		return
	}

	if len(nodeKeys) < 2 {
		return
	}

	// ── Step 1: Record pre-recovery heights ──────────────────────────────────
	preHeights := queryNodeHeights()
	preMax := maxEpoch(preHeights)
	if preMax == 0 {
		log.Println("[quiet-recovery] no responsive nodes, skipping")
		return
	}
	for name, h := range preHeights {
		log.Printf("[quiet-recovery] pre-height %s: %d", name, h)
	}
	log.Printf("[quiet-recovery] pre-recovery max height: %d", preMax)

	// ── Step 2: Pause fault injection ────────────────────────────────────────
	log.Printf("[quiet-recovery] requesting %ss quiet period via: %s", quietDuration, stopBin)
	cmd := exec.CommandContext(ctx, stopBin, quietDuration)
	if err := cmd.Run(); err != nil {
		log.Printf("[quiet-recovery] ANTITHESIS_STOP_FAULTS failed: %v", err)
		return
	}

	// ── Step 3: Wait for stabilization ───────────────────────────────────────
	time.Sleep(time.Duration(quietStabilizeSecs) * time.Second)

	// ── Step 4: Record post-recovery heights ─────────────────────────────────
	postHeights := queryNodeHeights()
	postMax := maxEpoch(postHeights)
	postMin := minEpoch(postHeights)
	for name, h := range postHeights {
		log.Printf("[quiet-recovery] post-height %s: %d", name, h)
	}
	log.Printf("[quiet-recovery] post-recovery max height: %d, min height: %d", postMax, postMin)

	// ── Step 5: Assert chain advanced ────────────────────────────────────────
	advanced := postMax > preMax
	assert.Sometimes(advanced, "Chain advanced during quiet period", map[string]any{
		"pre_max_height":  preMax,
		"post_max_height": postMax,
	})
	if advanced {
		log.Printf("[quiet-recovery] chain advanced from %d to %d", preMax, postMax)
	} else {
		log.Printf("[quiet-recovery] chain did NOT advance (pre=%d post=%d)", preMax, postMax)
	}

	// ── Step 6: Assert consensus recovery (drift check) ──────────────────────
	if len(postHeights) < 2 {
		log.Println("[quiet-recovery] fewer than 2 responsive nodes post-recovery, skipping convergence check")
		return
	}

	drift := int(postMax - postMin)
	converged := drift <= quietDriftThreshold
	assert.Sometimes(converged, "Consensus recovered during quiet period", map[string]any{
		"drift":     drift,
		"threshold": quietDriftThreshold,
		"nodes":     len(postHeights),
	})

	if converged {
		log.Printf("[quiet-recovery] consensus recovered (drift=%d <= %d)", drift, quietDriftThreshold)
	} else {
		log.Printf("[quiet-recovery] consensus NOT recovered (drift=%d > %d)", drift, quietDriftThreshold)
		return // don't check tipset agreement when nodes are diverged
	}

	// ── Step 7: Assert tipset agreement at finalized height ──────────────────
	// Use the minimum post-recovery height minus a small finality buffer as the
	// comparison point. All converged nodes should agree on this tipset.
	checkHeight := postMin - abi.ChainEpoch(quietFinalityBuffer)
	if checkHeight <= 0 {
		log.Println("[quiet-recovery] chain too short for finalized tipset check")
		return
	}

	var cidStrings []string
	var cidByNode []string // "node=CIDs" for logging
	var respondents int
	for _, name := range nodeKeys {
		h, ok := postHeights[name]
		if !ok || h == 0 {
			continue
		}
		ts, err := nodes[name].ChainGetTipSetByHeight(ctx, checkHeight, types.EmptyTSK)
		if err != nil {
			debugLog("[quiet-recovery] ChainGetTipSetByHeight(%d) failed on %s: %v", checkHeight, name, err)
			continue
		}
		cids := ""
		for _, c := range ts.Cids() {
			cids += c.String() + ","
		}
		cidStrings = append(cidStrings, cids)
		cidByNode = append(cidByNode, name+"="+cids)
		respondents++
	}
	log.Printf("[quiet-recovery] tipsets at height %d: %v", checkHeight, cidByNode)


	if respondents < 2 {
		log.Printf("[quiet-recovery] only %d nodes returned tipsets at height %d, skipping agreement check", respondents, checkHeight)
		return
	}

	allAgree := true
	for i := 1; i < len(cidStrings); i++ {
		if cidStrings[i] != cidStrings[0] {
			allAgree = false
			break
		}
	}

	assert.Always(allAgree, "State consistent after quiet period recovery", map[string]any{
		"check_height": checkHeight,
		"respondents":  respondents,
		"drift":        drift,
	})

	if allAgree {
		log.Printf("[quiet-recovery] all %d nodes agree on tipset at height %d", respondents, checkHeight)
	} else {
		log.Printf("[quiet-recovery] TIPSET DISAGREEMENT at height %d among %d nodes", checkHeight, respondents)
		for _, entry := range cidByNode {
			log.Printf("[quiet-recovery]   %s", entry)
		}
		// Also check head tipset to see if disagreement extends to the tip
		for _, name := range nodeKeys {
			head, err := nodes[name].ChainHead(ctx)
			if err != nil {
				continue
			}
			headCids := ""
			for _, c := range head.Cids() {
				headCids += c.String() + ","
			}
			log.Printf("[quiet-recovery] head-tipset %s (height=%d): %s", name, head.Height(), headCids)
		}
	}
}

// queryNodeHeights returns the chain head height for each connected node.
func queryNodeHeights() map[string]abi.ChainEpoch {
	heights := make(map[string]abi.ChainEpoch, len(nodeKeys))
	for _, name := range nodeKeys {
		head, err := nodes[name].ChainHead(ctx)
		if err != nil {
			debugLog("[quiet-recovery] ChainHead failed on %s: %v", name, err)
			continue
		}
		heights[name] = head.Height()
	}
	return heights
}

// maxEpoch returns the maximum height from a height map.
func maxEpoch(heights map[string]abi.ChainEpoch) abi.ChainEpoch {
	var max abi.ChainEpoch
	for _, h := range heights {
		if h > max {
			max = h
		}
	}
	return max
}

// minEpoch returns the minimum height from a height map (ignoring zeros).
func minEpoch(heights map[string]abi.ChainEpoch) abi.ChainEpoch {
	var min abi.ChainEpoch
	for _, h := range heights {
		if h > 0 && (min == 0 || h < min) {
			min = h
		}
	}
	return min
}
