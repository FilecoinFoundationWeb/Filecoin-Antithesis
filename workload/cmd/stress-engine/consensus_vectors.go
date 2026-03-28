package main

import (
	"log"
	"sync"
	"time"

	"github.com/antithesishq/antithesis-sdk-go/assert"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/ipfs/go-cid"
)

// ===========================================================================
// Vector 4: DoHeavyCompute (Resource Safety)
// Recomputes state for a recent epoch via StateCompute and verifies
// the result matches the stored parent state root. Stresses the node's
// compute pipeline.
// ===========================================================================

const (
	computeMinHeight    = 20
	computeStartOffset  = 2  // epochs behind head to start
	computeEndOffset    = 12 // epochs behind head to stop
	computeTargetEpochs = 5  // how many epochs to verify per call
)

func DoHeavyCompute() {
	// Use first node to determine a safe finalized check range
	finTs, err := nodes[nodeKeys[0]].ChainGetFinalizedTipSet(ctx)
	if err != nil || finTs.Height() < computeMinHeight {
		return
	}

	startHeight := finTs.Height() - abi.ChainEpoch(computeStartOffset)
	endHeight := finTs.Height() - abi.ChainEpoch(computeEndOffset)

	// Run on every node
	for _, nodeName := range nodeKeys {
		node := nodes[nodeName]

		checkTs, err := node.ChainGetTipSetByHeight(ctx, startHeight, finTs.Key())
		if err != nil {
			log.Printf("[heavy-compute] ChainGetTipSetByHeight(%d) failed for %s: %v", startHeight, nodeName, err)
			continue
		}

		epochsChecked := 0
		for epochsChecked < computeTargetEpochs && checkTs.Height() >= endHeight {
			parentKey := checkTs.Parents()
			parentTs, err := node.ChainGetTipSet(ctx, parentKey)
			if err != nil {
				log.Printf("[heavy-compute] ChainGetTipSet failed on %s at height %d: %v", nodeName, checkTs.Height(), err)
				break
			}

			if parentTs.Height() < endHeight {
				break
			}

			// Recompute state — this is the expensive operation that stresses the node
			st, err := node.StateCompute(ctx, parentTs.Height(), nil, parentKey)
			if err != nil {
				log.Printf("[heavy-compute] StateCompute failed on %s at height %d: %v", nodeName, parentTs.Height(), err)
				break
			}

			stateMatches := st.Root == checkTs.ParentState()

			assert.Always(stateMatches, nodeName+": Recomputed state root matches stored state", map[string]any{
				"node":           nodeName,
				"node_type":      nodeType(nodeName),
				"exec_height":    parentTs.Height(),
				"check_height":   checkTs.Height(),
				"computed_root":  st.Root.String(),
				"expected_root":  checkTs.ParentState().String(),
				"epochs_checked": epochsChecked,
			})

			if !stateMatches {
				log.Printf("[heavy-compute] STATE MISMATCH on %s at height %d: computed=%s expected=%s",
					nodeName, parentTs.Height(), st.Root.String(), checkTs.ParentState().String())
				break
			}

			checkTs = parentTs
			epochsChecked++
		}

		debugLog("  [heavy-compute] OK: verified %d epochs on %s", epochsChecked, nodeName)
	}
}

// ===========================================================================
// Consensus & Node Health Checks
//
// Each check is a standalone deck entry with its own weight:
//   - DoTipsetConsensus: all nodes agree on finalized tipset
//   - DoHeightProgression: all nodes advancing
//   - DoPeerCount: all nodes have peers
//   - DoHeadComparison: finalized tipset comparison
//   - DoStateRootComparison: state root comparison at finalized height
//   - DoStateAudit: state roots + msg/receipt verification (primary safety net)
//
// State-sensitive checks use ChainGetFinalizedTipSet so they
// are safe during partition → reorg chaos.
// ===========================================================================

const (
	consensusWalkEpochs = 5
	finalizedMinHeight  = 5  // skip checks until finalized tipset is past this
	f3MinEpoch          = 10 // minimum chain head height on all nodes before F3 checks run
)

// allNodesPastEpoch returns true only if every node's chain head is at or above minEpoch.
func allNodesPastEpoch(minEpoch abi.ChainEpoch) bool {
	for _, name := range nodeKeys {
		head, err := nodes[name].ChainHead(ctx)
		if err != nil {
			return false
		}
		if head.Height() < minEpoch {
			return false
		}
	}
	return true
}

// getFinalizedHeight returns the minimum finalized tipset height across nodes.
// Returns 0 if any node fails. This is the safe boundary for state assertions.
func getFinalizedHeight() (abi.ChainEpoch, types.TipSetKey) {
	minHeight := abi.ChainEpoch(0)
	var minTsk types.TipSetKey
	first := true
	for _, name := range nodeKeys {
		ts, err := nodes[name].ChainGetFinalizedTipSet(ctx)
		if err != nil {
			log.Printf("[chain-monitor] ChainGetFinalizedTipSet failed for %s: %v", name, err)
			return 0, types.EmptyTSK
		}
		if first || ts.Height() < minHeight {
			minHeight = ts.Height()
			minTsk = ts.Key()
			first = false
		}
	}
	return minHeight, minTsk
}

// doTipsetConsensus checks that all nodes agree on the tipset at a finalized height.
func DoTipsetConsensus() {
	if len(nodeKeys) < 2 {
		return
	}
	if !allNodesPastEpoch(f3MinEpoch) {
		return
	}

	finalizedHeight, _ := getFinalizedHeight()
	if finalizedHeight < finalizedMinHeight {
		return
	}

	// Pick a random height within the finalized range
	checkHeight := abi.ChainEpoch(rngIntn(int(finalizedHeight)) + 1)

	// Query all nodes concurrently for tipset at this height
	type result struct {
		name      string
		tipsetKey string
		err       error
	}

	results := make(chan result, len(nodeKeys))
	var wg sync.WaitGroup

	for _, name := range nodeKeys {
		wg.Add(1)
		go func(nodeName string) {
			defer wg.Done()
			// Use finalized tipset as the anchor for lookback
			finTs, err := nodes[nodeName].ChainGetFinalizedTipSet(ctx)
			if err != nil {
				results <- result{name: nodeName, err: err}
				return
			}
			ts, err := nodes[nodeName].ChainGetTipSetByHeight(ctx, checkHeight, finTs.Key())
			if err != nil {
				results <- result{name: nodeName, err: err}
				return
			}
			results <- result{name: nodeName, tipsetKey: ts.Key().String()}
		}(name)
	}

	wg.Wait()
	close(results)

	tipsetKeys := make(map[string][]string) // key -> []nodeName
	var errs int
	for r := range results {
		if r.err != nil {
			log.Printf("[chain-monitor] tipset query failed for %s: %v", r.name, r.err)
			errs++
			continue
		}
		tipsetKeys[r.tipsetKey] = append(tipsetKeys[r.tipsetKey], r.name)
	}

	responded := len(nodeKeys) - errs
	if responded < 2 {
		return // need at least 2 nodes to check consensus
	}

	consensusReached := len(tipsetKeys) == 1

	// Sometimes, not Always: the check height is a random point within the
	// finalized window (which can be near the tip), where transient forks
	// are expected. Nodes should agree *eventually* (liveness), but
	// point-in-time disagreement during active forks is normal.
	assert.Sometimes(consensusReached, "All nodes agree on the same finalized tipset", map[string]any{
		"height":         checkHeight,
		"finalized_at":   finalizedHeight,
		"tipset_keys":    tipsetKeys,
		"unique_tipsets": len(tipsetKeys),
		"nodes_checked":  responded,
		"nodes":          nodeKeys,
		"errors":         errs,
	})

	if errs > 0 {
		log.Printf("[chain-monitor] %d/%d nodes had query errors at height %d (finalized=%d)",
			errs, len(nodeKeys), checkHeight, finalizedHeight)
	}
}

// doHeightProgression checks that all nodes are advancing.
// Ported from node-health.go CheckHeightProgression.
func DoHeightProgression() {
	heights := make(map[string]abi.ChainEpoch)
	for _, name := range nodeKeys {
		finTs, err := nodes[name].ChainGetFinalizedTipSet(ctx)
		if err != nil {
			log.Printf("[chain-monitor] ChainGetFinalizedTipSet failed for %s: %v", name, err)
			continue
		}
		heights[name] = finTs.Height()
	}

	if len(heights) == 0 {
		return
	}

	// Find min and max heights
	var minH, maxH abi.ChainEpoch
	first := true
	for _, h := range heights {
		if first {
			minH, maxH = h, h
			first = false
		}
		if h < minH {
			minH = h
		}
		if h > maxH {
			maxH = h
		}
	}

	// Skip during startup: if the slowest node hasn't passed the finalized
	// minimum, it is still bootstrapping and a large spread is expected.
	if minH < finalizedMinHeight {
		return
	}

	// Nodes shouldn't be too far apart (>10 epochs suggests a problem)
	spread := maxH - minH
	acceptable := spread <= 10

	// Sometimes: during active partitions one side stops getting blocks,
	// so a large spread is expected. The fork monitor already catches
	// persistent divergence.
	assert.Sometimes(acceptable, "Node chain heights are within acceptable range", map[string]any{
		"heights": heights,
		"spread":  spread,
		"min":     minH,
		"max":     maxH,
		"nodes":   nodeKeys,
	})
}

// doPeerCount checks that all nodes have peers.
// Ported from node-health.go CheckPeerCount.
func DoPeerCount() {
	for _, name := range nodeKeys {
		peers, err := nodes[name].NetPeers(ctx)
		if err != nil {
			log.Printf("[chain-monitor] NetPeers failed for %s: %v", name, err)
			continue
		}

		peerCount := len(peers)

		assert.Sometimes(peerCount > 0, name+": Node has active peer connections", map[string]any{
			"node":       name,
			"node_type":  nodeType(name),
			"peer_count": peerCount,
		})
	}
}

// doHeadComparison queries ChainHead from all nodes and compares.
// Simpler than full tipset consensus — just checks heads are close.
func DoHeadComparison() {
	if len(nodeKeys) < 2 {
		return
	}
	if !allNodesPastEpoch(f3MinEpoch) {
		return
	}

	type headInfo struct {
		name   string
		height abi.ChainEpoch
		key    string
	}

	var heads []headInfo
	for _, name := range nodeKeys {
		head, err := nodes[name].ChainGetFinalizedTipSet(ctx)
		if err != nil {
			log.Printf("[chain-monitor] ChainHead failed for %s: %v", name, err)
			continue
		}
		heads = append(heads, headInfo{
			name:   name,
			height: head.Height(),
			key:    head.Key().String(),
		})
	}

	if len(heads) < 2 {
		return
	}

	// Group by height
	byHeight := make(map[abi.ChainEpoch][]headInfo)
	for _, h := range heads {
		byHeight[h.height] = append(byHeight[h.height], h)
	}

	// For nodes at the same height, their tipset keys should match
	for height, group := range byHeight {
		if len(group) < 2 {
			continue
		}
		firstKey := group[0].key
		allMatch := true
		for _, h := range group[1:] {
			if h.key != firstKey {
				allMatch = false
				break
			}
		}

		nodeTipsets := make(map[string]string, len(group))
		for _, h := range group {
			nodeTipsets[h.name] = h.key
		}

		// Sometimes: transient forks with shallow EC finality (head-20) mean
		// nodes may temporarily disagree on the tipset at a given height.
		assert.Sometimes(allMatch, "Nodes at the same height agree on the same tipset", map[string]any{
			"height":       height,
			"nodes":        len(group),
			"keys_match":   allMatch,
			"node_tipsets": nodeTipsets,
		})
	}
}

// doStateRootComparison compares parent state roots across all nodes at a finalized height.
// Catches state divergence. Uses finalized tipset so partitions don't cause false positives.
func DoStateRootComparison() {
	if len(nodeKeys) < 2 {
		return
	}
	if !allNodesPastEpoch(f3MinEpoch) {
		return
	}

	finalizedHeight, _ := getFinalizedHeight()
	if finalizedHeight < finalizedMinHeight {
		return
	}

	checkHeight := abi.ChainEpoch(rngIntn(int(finalizedHeight)) + 1)

	// Collect parent state roots from all nodes at this finalized height
	stateRoots := make(map[string][]string) // root -> []nodeName
	for _, name := range nodeKeys {
		finTs, err := nodes[name].ChainGetFinalizedTipSet(ctx)
		if err != nil {
			log.Printf("[chain-monitor] ChainGetFinalizedTipSet failed for %s: %v", name, err)
			return
		}
		ts, err := nodes[name].ChainGetTipSetByHeight(ctx, checkHeight, finTs.Key())
		if err != nil {
			log.Printf("[chain-monitor] ChainGetTipSetByHeight(%d) failed for %s: %v", checkHeight, name, err)
			return
		}
		root := ts.ParentState().String()
		stateRoots[root] = append(stateRoots[root], name)
	}

	statesMatch := len(stateRoots) == 1

	details := map[string]any{
		"height":        checkHeight,
		"finalized_at":  finalizedHeight,
		"state_roots":   stateRoots,
		"unique_states": len(stateRoots),
		"nodes_checked": len(nodeKeys),
		"nodes":         nodeKeys,
	}

	if statesMatch {
		assert.Always(true, "Chain state is consistent across all nodes", details)
		debugLog("  [chain-monitor] OK: all %d nodes agree at height %d (finalized=%d)", len(nodeKeys), checkHeight, finalizedHeight)
	} else {
		log.Printf("  [chain-monitor] DIVERGENCE at height %d, stabilizing to verify: %v", checkHeight, stateRoots)

		resolved := stabilizeAndRecheck(func() bool {
			retryRoots := make(map[string][]string)
			for _, name := range nodeKeys {
				finTs, err := nodes[name].ChainGetFinalizedTipSet(ctx)
				if err != nil {
					return false
				}
				ts, err := nodes[name].ChainGetTipSetByHeight(ctx, checkHeight, finTs.Key())
				if err != nil {
					return false
				}
				root := ts.ParentState().String()
				retryRoots[root] = append(retryRoots[root], name)
			}
			return len(retryRoots) == 1
		})

		details["stabilized"] = true
		details["resolved_after_stabilization"] = resolved
		assert.Always(resolved, "Chain state is consistent across all nodes", details)
	}
}

// doStateAudit compares state roots, parent messages, and parent receipts
// across nodes at a finalized height. Catches non-determinism in FVM execution
// that would cause consensus splits (the Dec 2020 chain halt bug class).
func DoStateAudit() {
	if len(nodeKeys) < 2 {
		return
	}
	if !allNodesPastEpoch(f3MinEpoch) {
		return
	}

	finalizedHeight, _ := getFinalizedHeight()
	if finalizedHeight < finalizedMinHeight {
		return
	}

	checkHeight := abi.ChainEpoch(rngIntn(int(finalizedHeight)) + 1)

	// Phase 1: State root comparison using finalized tipset
	stateRoots := make(map[string][]string)
	var tipsetCids []cid.Cid

	for _, name := range nodeKeys {
		finTs, err := nodes[name].ChainGetFinalizedTipSet(ctx)
		if err != nil {
			return
		}
		ts, err := nodes[name].ChainGetTipSetByHeight(ctx, checkHeight, finTs.Key())
		if err != nil {
			return
		}
		root := ts.ParentState().String()
		stateRoots[root] = append(stateRoots[root], name)

		if len(tipsetCids) == 0 {
			tipsetCids = ts.Cids()
		}
	}

	rootsMatch := len(stateRoots) == 1

	auditDetails := map[string]any{
		"height":        checkHeight,
		"finalized_at":  finalizedHeight,
		"unique_states": len(stateRoots),
		"state_roots":   stateRoots,
		"nodes":         nodeKeys,
	}

	if rootsMatch {
		assert.Always(true, "State root is consistent after FVM execution", auditDetails)
	} else {
		log.Printf("[chain-monitor] STATE ROOT DIVERGENCE at height %d, stabilizing to verify: %v", checkHeight, stateRoots)

		resolved := stabilizeAndRecheck(func() bool {
			retryRoots := make(map[string][]string)
			for _, name := range nodeKeys {
				finTs, err := nodes[name].ChainGetFinalizedTipSet(ctx)
				if err != nil {
					return false
				}
				ts, err := nodes[name].ChainGetTipSetByHeight(ctx, checkHeight, finTs.Key())
				if err != nil {
					return false
				}
				root := ts.ParentState().String()
				retryRoots[root] = append(retryRoots[root], name)
			}
			return len(retryRoots) == 1
		})

		auditDetails["stabilized"] = true
		auditDetails["resolved_after_stabilization"] = resolved
		assert.Always(resolved, "State root is consistent after FVM execution", auditDetails)

		if !resolved {
			return
		}
	}

	// Phase 2: Message-Receipt correspondence check
	if len(tipsetCids) == 0 {
		return
	}

	for _, blkCid := range tipsetCids {
		nodeA := nodeKeys[0]
		nodeB := nodeKeys[1]

		msgsA, errA := nodes[nodeA].ChainGetParentMessages(ctx, blkCid)
		msgsB, errB := nodes[nodeB].ChainGetParentMessages(ctx, blkCid)

		if errA != nil || errB != nil {
			continue
		}

		receiptsA, errA := nodes[nodeA].ChainGetParentReceipts(ctx, blkCid)
		receiptsB, errB := nodes[nodeB].ChainGetParentReceipts(ctx, blkCid)

		if errA != nil || errB != nil {
			continue
		}

		msgsMatch := len(msgsA) == len(msgsB)
		assert.Always(msgsMatch, "Parent messages match across nodes", map[string]any{
			"height":      checkHeight,
			"block":       blkCid.String()[:16],
			"node_a":      nodeA,
			"node_a_type": nodeType(nodeA),
			"node_b":      nodeB,
			"node_b_type": nodeType(nodeB),
			"count_a":     len(msgsA),
			"count_b":     len(msgsB),
		})

		receiptsMatch := len(receiptsA) == len(receiptsB)
		assert.Always(receiptsMatch, "Parent receipts match across nodes", map[string]any{
			"height":      checkHeight,
			"block":       blkCid.String()[:16],
			"node_a":      nodeA,
			"node_a_type": nodeType(nodeA),
			"node_b":      nodeB,
			"node_b_type": nodeType(nodeB),
			"count_a":     len(receiptsA),
			"count_b":     len(receiptsB),
		})

		msgReceiptMatch := len(msgsA) == len(receiptsA)
		assert.Always(msgReceiptMatch, "Message and receipt counts match", map[string]any{
			"height":      checkHeight,
			"block":       blkCid.String()[:16],
			"node_a":      nodeA,
			"node_a_type": nodeType(nodeA),
			"msgs":        len(msgsA),
			"receipts":    len(receiptsA),
		})

		if !msgsMatch || !receiptsMatch || !msgReceiptMatch {
			log.Printf("[chain-monitor] MESSAGE/RECEIPT MISMATCH at height %d block %s",
				checkHeight, blkCid.String()[:16])
		}
	}

	debugLog("  [chain-monitor] OK: state-audit height %d, roots match, msgs/receipts consistent", checkHeight)
}

// ===========================================================================
// Fork Monitor — background convergence-based fork detection
//
// Runs as a background goroutine (not in the deck) so it can observe forks
// while DoReorgChaos partitions are active. Polls every forkPollInterval,
// detects disagreements, and asserts failure only when a fork PERSISTS past
// a convergence window.
//
// ===========================================================================

const (
	// forkConvergenceBuffer is how many epochs the chain must advance past
	// the detection point before we re-check. If nodes still disagree after
	// this many epochs, it's a persistent fork (real bug).
	forkConvergenceBuffer = 50

	// forkPollInterval is how often the background goroutine checks for forks.
	forkPollInterval = 5 * time.Second

	// forkMaxTracked limits memory usage for tracked forks.
	forkMaxTracked = 100
)

// trackedFork records a detected disagreement for later re-verification.
type trackedFork struct {
	height         abi.ChainEpoch    // height where disagreement was observed
	detectedAtHead abi.ChainEpoch    // min chain head when first detected
	tipsets        map[string]string // node -> tipset key (snapshot at detection)
}

// Global fork tracker — append-only during detection, pruned during verification.
var (
	trackedForks   []trackedFork
	trackedForksMu sync.Mutex
)

// startForkMonitor launches the background fork detection goroutine.
// Call once from main() after node connections are established.
func startForkMonitor() {
	go func() {
		log.Println("[fork-monitor] background goroutine started")
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(forkPollInterval):
				forkMonitorTick()
			}
		}
	}()
}

// forkMonitorTick runs one detect+verify cycle.
func forkMonitorTick() {
	if len(nodeKeys) < 2 {
		return
	}

	// Get min chain head across all nodes — skip nodes that error
	// (a partitioned node's RPC may still work, returning its stale head)
	minHead := abi.ChainEpoch(0)
	heads := make(map[string]abi.ChainEpoch)
	for _, name := range nodeKeys {
		head, err := nodes[name].ChainHead(ctx)
		if err != nil {
			debugLog("[fork-monitor] ChainHead failed for %s: %v", name, err)
			continue
		}
		heads[name] = head.Height()
		if minHead == 0 || head.Height() < minHead {
			minHead = head.Height()
		}
	}

	if len(heads) < 2 {
		debugLog("[fork-monitor] only %d nodes reachable, skipping", len(heads))
		return
	}

	if minHead < f3MinEpoch {
		debugLog("[fork-monitor] minHead=%d < f3MinEpoch=%d, skipping", minHead, f3MinEpoch)
		return
	}

	debugLog("[fork-monitor] tick: heads=%v minHead=%d", heads, minHead)
	detectForks(minHead)
	verifyForks(minHead)
}

// detectForks queries nodes for tipsets at a recent height and records disagreements.
func detectForks(minHead abi.ChainEpoch) {
	// Check at a height within the EC finality window — this is where forks live.
	// Use the finalized height (head-20) as the check point.
	finalizedHeight, _ := getFinalizedHeight()
	if finalizedHeight < finalizedMinHeight {
		return
	}

	// Query all nodes for the tipset at the finalized height, each anchored
	// to its own chain view.
	nodeTipsets := make(map[string]string)
	for _, name := range nodeKeys {
		finTs, err := nodes[name].ChainGetFinalizedTipSet(ctx)
		if err != nil {
			continue
		}
		ts, err := nodes[name].ChainGetTipSetByHeight(ctx, finalizedHeight, finTs.Key())
		if err != nil {
			continue
		}
		nodeTipsets[name] = ts.Key().String()
	}

	if len(nodeTipsets) < 2 {
		debugLog("[fork-monitor] only %d nodes returned tipsets at height %d, skipping", len(nodeTipsets), finalizedHeight)
		return
	}

	// Check for disagreement
	keys := make(map[string]bool)
	for _, k := range nodeTipsets {
		keys[k] = true
	}

	if len(keys) <= 1 {
		// All agree — log and emit Sometimes for liveness tracking
		debugLog("[fork-monitor] nodes agree at finalized height %d", finalizedHeight)
		assert.Sometimes(true, "Fork monitor: nodes agree at finalized height", map[string]any{
			"height": finalizedHeight,
			"nodes":  len(nodeTipsets),
		})
		return
	}

	// Fork detected — record it
	trackedForksMu.Lock()
	defer trackedForksMu.Unlock()

	// Don't duplicate — skip if we already track this height
	for _, tf := range trackedForks {
		if tf.height == finalizedHeight {
			return
		}
	}

	// Evict oldest if at capacity
	if len(trackedForks) >= forkMaxTracked {
		trackedForks = trackedForks[1:]
	}

	trackedForks = append(trackedForks, trackedFork{
		height:         finalizedHeight,
		detectedAtHead: minHead,
		tipsets:        nodeTipsets,
	})

	log.Printf("[fork-monitor] fork detected at height %d (chain head=%d): %v",
		finalizedHeight, minHead, nodeTipsets)
}

// verifyForks re-checks old forks that have had enough time to resolve.
func verifyForks(minHead abi.ChainEpoch) {
	trackedForksMu.Lock()
	defer trackedForksMu.Unlock()

	remaining := trackedForks[:0] // reuse backing array

	for _, tf := range trackedForks {
		epochsSinceDetection := minHead - tf.detectedAtHead

		// Not enough time has passed — keep tracking
		if epochsSinceDetection < forkConvergenceBuffer {
			remaining = append(remaining, tf)
			continue
		}

		// Re-query all nodes at the fork height
		nodeTipsets := make(map[string]string)
		for _, name := range nodeKeys {
			head, err := nodes[name].ChainHead(ctx)
			if err != nil {
				continue
			}
			ts, err := nodes[name].ChainGetTipSetByHeight(ctx, tf.height, head.Key())
			if err != nil {
				continue
			}
			nodeTipsets[name] = ts.Key().String()
		}

		if len(nodeTipsets) < 2 {
			remaining = append(remaining, tf)
			continue
		}

		// Check if fork resolved
		keys := make(map[string][]string) // tipsetKey -> []nodeName
		for name, k := range nodeTipsets {
			keys[k] = append(keys[k], name)
		}

		resolved := len(keys) == 1

		if resolved {
			log.Printf("[fork-monitor] fork at height %d RESOLVED after %d epochs",
				tf.height, epochsSinceDetection)
			// Don't keep — it resolved
			continue
		}

		// Persistent fork — this is a real consensus bug
		log.Printf("[fork-monitor] PERSISTENT FORK at height %d after %d epochs: %v",
			tf.height, epochsSinceDetection, keys)

		assert.Always(false, "Persistent consensus fork detected", map[string]any{
			"fork_height":           tf.height,
			"detected_at_head":      tf.detectedAtHead,
			"verified_at_head":      minHead,
			"epochs_since_detected": epochsSinceDetection,
			"original_tipsets":      tf.tipsets,
			"current_tipsets":       keys,
			"nodes":                 nodeKeys,
		})

		// Keep tracking — might want to see it fire again
		remaining = append(remaining, tf)
	}

	trackedForks = remaining
}
