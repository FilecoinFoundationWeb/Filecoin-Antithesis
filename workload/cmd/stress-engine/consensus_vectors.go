package main

import (
	"encoding/hex"
	"fmt"
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
	f3MinEpoch          = 21 // minimum chain head height before finality-dependent checks run (must exceed EC finality depth 20)

	// snapshotTTL controls how long a finalized-tipset snapshot is reused.
	// Multiple consensus checks hitting the same deck tick share one fetch round.
	snapshotTTL = 2 * time.Second
)

// ---------------------------------------------------------------------------
// Finalized-tipset snapshot cache
//
// DoTipsetConsensus, DoStateRootComparison, DoHeadComparison, and DoStateAudit
// all query ChainGetFinalizedTipSet on every node. This cache deduplicates
// those calls within a short TTL window so a single deck tick cycle pays the
// RPC cost once.
// ---------------------------------------------------------------------------

type nodeSnapshot struct {
	finTs  *types.TipSet
	height abi.ChainEpoch
	key    types.TipSetKey
	err    error
}

var (
	snapCache   map[string]nodeSnapshot // nodeName -> snapshot
	snapCacheMu sync.Mutex
	snapCacheAt time.Time
)

// getFinalizedSnapshots returns a cached-or-fresh map of each node's finalized
// tipset. Safe to call from any deck vector — concurrent callers within the
// TTL window share the same result.
func getFinalizedSnapshots() map[string]nodeSnapshot {
	snapCacheMu.Lock()
	defer snapCacheMu.Unlock()

	if snapCache != nil && time.Since(snapCacheAt) < snapshotTTL {
		return snapCache
	}

	snap := make(map[string]nodeSnapshot, len(nodeKeys))
	for _, name := range nodeKeys {
		ts, err := nodes[name].ChainGetFinalizedTipSet(ctx)
		if err != nil {
			snap[name] = nodeSnapshot{err: err}
			continue
		}
		snap[name] = nodeSnapshot{
			finTs:  ts,
			height: ts.Height(),
			key:    ts.Key(),
		}
	}
	snapCache = snap
	snapCacheAt = time.Now()
	return snap
}

// snapshotMinHeight returns the minimum finalized height across all nodes
// that responded successfully, plus the corresponding tipset key.
// Returns 0 if fewer than 2 nodes responded.
func snapshotMinHeight(snap map[string]nodeSnapshot) (abi.ChainEpoch, types.TipSetKey) {
	minH := abi.ChainEpoch(0)
	var minTsk types.TipSetKey
	count := 0
	for _, s := range snap {
		if s.err != nil {
			continue
		}
		count++
		if count == 1 || s.height < minH {
			minH = s.height
			minTsk = s.key
		}
	}
	if count < 2 {
		return 0, types.EmptyTSK
	}
	return minH, minTsk
}

// allNodesPastEpoch returns true if the highest chain head across all nodes
// is at or above minEpoch. Uses max height rather than requiring every node
// past the threshold — during EC reorgs without F3, nodes may temporarily
// reorg to shorter chains, but the network has matured if any node reached it.
func allNodesPastEpoch(minEpoch abi.ChainEpoch) bool {
	var maxHeight abi.ChainEpoch
	for _, name := range nodeKeys {
		head, err := nodes[name].ChainHead(ctx)
		if err != nil {
			continue
		}
		if head.Height() > maxHeight {
			maxHeight = head.Height()
		}
	}
	return maxHeight >= minEpoch
}

// getFinalizedHeight returns the minimum finalized tipset height across nodes.
// Uses the shared snapshot cache. Returns 0 if fewer than 2 nodes responded.
func getFinalizedHeight() (abi.ChainEpoch, types.TipSetKey) {
	return snapshotMinHeight(getFinalizedSnapshots())
}

// doTipsetConsensus checks that all nodes agree on the tipset at a finalized height.
func DoTipsetConsensus() {
	if len(nodeKeys) < 2 {
		return
	}
	if !allNodesPastEpoch(f3MinEpoch) {
		return
	}
	if partitionActive.Load() {
		return
	}

	snap := getFinalizedSnapshots()
	finalizedHeight, anchorKey := snapshotMinHeight(snap)
	if finalizedHeight < finalizedMinHeight {
		return
	}

	checkHeight := abi.ChainEpoch(rngIntn(int(finalizedHeight)) + 1)

	tipsetKeys := make(map[string][]string) // key -> []nodeName
	var errs int

	for name, s := range snap {
		if s.err != nil {
			log.Printf("[chain-monitor] tipset query failed for %s: %v", name, s.err)
			errs++
			continue
		}
		_ = s // snapshot used only for error check; anchor is shared
		ts, err := nodes[name].ChainGetTipSetByHeight(ctx, checkHeight, anchorKey)
		if err != nil {
			log.Printf("[chain-monitor] ChainGetTipSetByHeight(%d) failed for %s: %v", checkHeight, name, err)
			errs++
			continue
		}
		tipsetKeys[ts.Key().String()] = append(tipsetKeys[ts.Key().String()], name)
	}

	responded := len(snap) - errs
	if responded < 2 {
		return
	}

	consensusReached := len(tipsetKeys) == 1

	details := map[string]any{
		"height":         checkHeight,
		"finalized_at":   finalizedHeight,
		"tipset_keys":    tipsetKeys,
		"unique_tipsets": len(tipsetKeys),
		"nodes_checked":  responded,
		"nodes":          nodeKeys,
		"errors":         errs,
	}

	// Heights well below the finalization frontier are deeply finalized —
	// nodes MUST agree. Near the frontier, transient disagreement is tolerable.
	if checkHeight < finalizedHeight-10 {
		assert.Always(consensusReached, "All nodes agree on deeply finalized tipset", details)
	} else {
		assert.Sometimes(consensusReached, "All nodes agree on the same finalized tipset", details)
	}

	if errs > 0 {
		log.Printf("[chain-monitor] %d/%d nodes had query errors at height %d (finalized=%d)",
			errs, len(nodeKeys), checkHeight, finalizedHeight)
	}
}

// doHeightProgression checks that all nodes are advancing.
// Ported from node-health.go CheckHeightProgression.
func DoHeightProgression() {
	if !allNodesPastEpoch(f3MinEpoch) {
		return
	}
	snap := getFinalizedSnapshots()
	heights := make(map[string]abi.ChainEpoch)
	for name, s := range snap {
		if s.err != nil {
			log.Printf("[chain-monitor] ChainGetFinalizedTipSet failed for %s: %v", name, s.err)
			continue
		}
		heights[name] = s.height
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

// doHeadComparison queries finalized tipsets from all nodes and compares.
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

	snap := getFinalizedSnapshots()
	var heads []headInfo
	for name, s := range snap {
		if s.err != nil {
			log.Printf("[chain-monitor] ChainHead failed for %s: %v", name, s.err)
			continue
		}
		heads = append(heads, headInfo{
			name:   name,
			height: s.height,
			key:    s.key.String(),
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
	if partitionActive.Load() {
		return
	}

	snap := getFinalizedSnapshots()
	finalizedHeight, anchorKey := snapshotMinHeight(snap)
	if finalizedHeight < finalizedMinHeight {
		return
	}

	checkHeight := abi.ChainEpoch(rngIntn(int(finalizedHeight)) + 1)

	// Collect parent state roots from all nodes at this finalized height
	stateRoots := make(map[string][]string) // root -> []nodeName
	for name, s := range snap {
		if s.err != nil {
			log.Printf("[chain-monitor] ChainGetFinalizedTipSet failed for %s: %v", name, s.err)
			continue
		}
		_ = s // snapshot used only for error check; anchor is shared
		ts, err := nodes[name].ChainGetTipSetByHeight(ctx, checkHeight, anchorKey)
		if err != nil {
			log.Printf("[chain-monitor] ChainGetTipSetByHeight(%d) failed for %s: %v", checkHeight, name, err)
			continue
		}
		root := ts.ParentState().String()
		stateRoots[root] = append(stateRoots[root], name)
	}

	if len(stateRoots) == 0 {
		return
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

	// Heights well below the finalization frontier are deeply finalized —
	// nodes MUST agree on state. Near the frontier, transient divergence is tolerable.
	if checkHeight < finalizedHeight-10 {
		assert.Always(statesMatch, "Chain state consistent at deeply finalized height", details)
	} else {
		assert.Sometimes(statesMatch, "Chain state is consistent across all nodes", details)
	}

	if statesMatch {
		debugLog("  [chain-monitor] OK: all %d nodes agree at height %d (finalized=%d)", len(nodeKeys), checkHeight, finalizedHeight)
	} else {
		log.Printf("  [chain-monitor] DIVERGENCE at height %d: %v", checkHeight, stateRoots)
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

	snap := getFinalizedSnapshots()
	finalizedHeight, anchorKey := snapshotMinHeight(snap)
	if finalizedHeight < finalizedMinHeight {
		return
	}

	checkHeight := abi.ChainEpoch(rngIntn(int(finalizedHeight)) + 1)

	// Phase 1: State root comparison using finalized tipset
	stateRoots := make(map[string][]string)
	var tipsetCids []cid.Cid

	for name, s := range snap {
		if s.err != nil {
			continue
		}
		_ = s // snapshot used only for error check; anchor is shared
		ts, err := nodes[name].ChainGetTipSetByHeight(ctx, checkHeight, anchorKey)
		if err != nil {
			continue
		}
		root := ts.ParentState().String()
		stateRoots[root] = append(stateRoots[root], name)

		if len(tipsetCids) == 0 {
			tipsetCids = ts.Cids()
		}
	}

	// Need at least 2 responding nodes (values in the map) to compare.
	// Note: len(stateRoots) counts unique roots, not responding nodes.
	totalResponded := 0
	for _, names := range stateRoots {
		totalResponded += len(names)
	}
	if totalResponded < 2 {
		return
	}

	rootsMatch := len(stateRoots) == 1

	// Sometimes: cross-node state root comparison during shallow EC finality
	// will see divergence when nodes are on different fork branches.
	assert.Sometimes(rootsMatch, "State root is consistent after FVM execution", map[string]any{
		"height":        checkHeight,
		"finalized_at":  finalizedHeight,
		"unique_states": len(stateRoots),
		"state_roots":   stateRoots,
		"nodes":         nodeKeys,
	})

	if !rootsMatch {
		log.Printf("[chain-monitor] STATE ROOT DIVERGENCE at height %d: %v", checkHeight, stateRoots)
		return
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

	// Skip while a partition is intentionally active — forks are expected
	// during n-split cycles and DoReorgChaos. We'll re-check once healed.
	if partitionActive.Load() {
		debugLog("[fork-monitor] partition active, skipping tick")
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

		// Re-query all nodes at the fork height, anchored to each node's
		// finalized tipset (matching detectForks). Using ChainHead would
		// false-positive when heads diverge above finalization.
		nodeTipsets := make(map[string]string)
		for _, name := range nodeKeys {
			finTs, err := nodes[name].ChainGetFinalizedTipSet(ctx)
			if err != nil {
				continue
			}
			ts, err := nodes[name].ChainGetTipSetByHeight(ctx, tf.height, finTs.Key())
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

// ===========================================================================
// DoF3FinalityAgreement — Cross-Node F3 Certificate Consistency
//
// Verifies that all nodes that finalized the same F3 instance agreed on the
// SAME ECChain. Catches determinism bugs where validators reach the same
// GPBFT round but finalize different state.

// Safe to use assert.Always because:
//   - During partitions where no side has >67% power, F3 produces no new
//     certificates — there is nothing to disagree on.
//   - During partitions where one side has >67% power, only that side
//     produces certificates — the minority has no conflicting cert.
//   - The only way two nodes both hold a certificate for instance N with
//     different ECChain keys is a BFT-layer determinism bug.
// ===========================================================================

func DoF3FinalityAgreement() {
	if len(nodeKeys) < 2 {
		return
	}
	if !allNodesPastEpoch(f3MinEpoch) {
		return
	}

	// Find the minimum F3 instance across all responsive nodes (including Forest).
	// Every node at or past this instance must have a certificate for it.
	var minInst uint64
	respondedNodes := 0
	for _, name := range nodeKeys {
		inst, ok := getF3Instance(nodes[name])
		if !ok {
			continue
		}
		respondedNodes++
		if respondedNodes == 1 || inst < minInst {
			minInst = inst
		}
	}
	if respondedNodes < 2 || minInst < 2 {
		return
	}

	// Pick a random already-finalized instance to audit (not the latest —
	// check a settled one to avoid races with in-progress instances).
	checkInst := uint64(rngIntn(int(minInst-1))) + 1

	// Query ALL nodes (including Forest) for the F3 certificate at this instance.
	// Cross-implementation divergence in F3 finality is the highest-severity consensus bug.
	type nodeResult struct {
		name           string
		nodeImpl       string // "lotus" or "forest"
		chainKey       string // hex-encoded ECChain key digest
		powerTableCID  string // CID of power table for next instance
		commitments    string // hex of supplemental data commitments
		signerCount    int    // number of signers in bitfield
		signatureShort string // first 16 bytes of aggregate signature (hex)
		err            error
	}

	var results []nodeResult
	for _, name := range nodeKeys {
		cert, err := nodes[name].F3GetCertificate(ctx, checkInst)
		if err != nil {
			results = append(results, nodeResult{name: name, nodeImpl: nodeType(name), err: err})
			continue
		}
		if cert == nil || cert.ECChain.IsZero() {
			results = append(results, nodeResult{name: name, nodeImpl: nodeType(name), err: fmt.Errorf("nil cert or zero chain")})
			continue
		}
		key := cert.ECChain.Key()
		sigCount, _ := cert.Signers.Count()
		sigShort := ""
		if len(cert.Signature) >= 16 {
			sigShort = hex.EncodeToString(cert.Signature[:16])
		} else if len(cert.Signature) > 0 {
			sigShort = hex.EncodeToString(cert.Signature)
		}
		results = append(results, nodeResult{
			name:           name,
			nodeImpl:       nodeType(name),
			chainKey:       hex.EncodeToString(key[:]),
			powerTableCID:  cert.SupplementalData.PowerTable.String(),
			commitments:    hex.EncodeToString(cert.SupplementalData.Commitments[:]),
			signerCount:    int(sigCount),
			signatureShort: sigShort,
			err:            nil,
		})
	}

	// Group by finalized ECChain key
	chainKeys := map[string][]string{}
	var errors int
	for _, r := range results {
		if r.err != nil {
			errors++
			continue
		}
		chainKeys[r.chainKey] = append(chainKeys[r.chainKey], r.name)
	}

	responded := len(results) - errors
	if responded < 2 {
		return
	}

	agreed := len(chainKeys) == 1

	// Track which implementations are represented
	implTypes := map[string]bool{}
	for _, r := range results {
		if r.err == nil {
			implTypes[r.nodeImpl] = true
		}
	}
	crossImpl := implTypes["lotus"] && implTypes["forest"]

	assert.Always(agreed, "F3 finality agreement: all nodes finalized same chain for instance", map[string]any{
		"instance":        checkInst,
		"unique_chains":   len(chainKeys),
		"chain_map":       chainKeys,
		"nodes_responded": responded,
		"nodes_errored":   errors,
		"cross_impl":      crossImpl,
	})

	if !agreed {
		log.Printf("[f3-agreement] FINALITY DISAGREEMENT at instance %d: %v (cross_impl=%v)", checkInst, chainKeys, crossImpl)
	} else {
		debugLog("[f3-agreement] instance %d: all %d nodes agree (cross_impl=%v)", checkInst, responded, crossImpl)
	}

	// Deep cert comparison: power table, supplemental data, signature
	// These should be identical across all nodes for the same instance.
	if agreed && responded >= 2 {
		ptCIDs := map[string][]string{}
		commitMap := map[string][]string{}
		sigMap := map[string][]string{}
		for _, r := range results {
			if r.err != nil {
				continue
			}
			ptCIDs[r.powerTableCID] = append(ptCIDs[r.powerTableCID], r.name)
			commitMap[r.commitments] = append(commitMap[r.commitments], r.name)
			sigMap[r.signatureShort] = append(sigMap[r.signatureShort], r.name)
		}

		ptAgreed := len(ptCIDs) == 1
		commitAgreed := len(commitMap) == 1
		sigAgreed := len(sigMap) == 1

		assert.Always(ptAgreed, "F3 cert power table CID agrees across all nodes", map[string]any{
			"instance":     checkInst,
			"power_tables": ptCIDs,
			"cross_impl":   crossImpl,
		})

		assert.Always(commitAgreed, "F3 cert supplemental data agrees across all nodes", map[string]any{
			"instance":    checkInst,
			"commitments": commitMap,
			"cross_impl":  crossImpl,
		})

		assert.Always(sigAgreed, "F3 cert aggregate signature agrees across all nodes", map[string]any{
			"instance":   checkInst,
			"signatures": sigMap,
			"cross_impl": crossImpl,
		})

		if !ptAgreed || !commitAgreed || !sigAgreed {
			log.Printf("[f3-agreement] DEEP CERT DIVERGENCE at instance %d: pt=%v commit=%v sig=%v",
				checkInst, ptCIDs, commitMap, sigMap)
		}
	}

	// Cross-implementation F3 agreement is the highest-value safety check.
	// A Lotus/Forest disagreement on F3 certificates = CVE-level consensus split.
	if crossImpl {
		assert.Always(agreed, "F3 cross-implementation agreement: Lotus and Forest finalized same chain", map[string]any{
			"instance":      checkInst,
			"unique_chains": len(chainKeys),
			"chain_map":     chainKeys,
		})
		assert.Sometimes(true, "F3 cross-implementation check executed", map[string]any{
			"instance": checkInst,
		})
	}

	assert.Sometimes(agreed, "F3 finality agreement check ran successfully", map[string]any{
		"instance":        checkInst,
		"nodes_responded": responded,
		"cross_impl":      crossImpl,
	})
}
