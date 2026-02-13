package main

import (
	"log"
	"sync"

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
	nodeName, node := pickNode()

	head, err := node.ChainHead(ctx)
	if err != nil {
		log.Printf("[heavy-compute] ChainHead failed for %s: %v", nodeName, err)
		return
	}

	if head.Height() < computeMinHeight {
		return
	}

	startHeight := head.Height() - abi.ChainEpoch(computeStartOffset)
	endHeight := head.Height() - abi.ChainEpoch(computeEndOffset)

	checkTs, err := node.ChainGetTipSetByHeight(ctx, startHeight, head.Key())
	if err != nil {
		log.Printf("[heavy-compute] ChainGetTipSetByHeight(%d) failed: %v", startHeight, err)
		return
	}

	epochsChecked := 0
	for epochsChecked < computeTargetEpochs && checkTs.Height() >= endHeight {
		parentKey := checkTs.Parents()
		parentTs, err := node.ChainGetTipSet(ctx, parentKey)
		if err != nil {
			log.Printf("[heavy-compute] ChainGetTipSet failed at height %d: %v", checkTs.Height(), err)
			return
		}

		if parentTs.Height() < endHeight {
			break
		}

		// Recompute state — this is the expensive operation that stresses the node
		st, err := node.StateCompute(ctx, parentTs.Height(), nil, parentKey)
		if err != nil {
			log.Printf("[heavy-compute] StateCompute failed at height %d: %v", parentTs.Height(), err)
			// Expected: node might reject if overloaded, that's not a safety violation
			return
		}

		stateMatches := st.Root == checkTs.ParentState()

		assert.Always(stateMatches, "state_computation_consistent", map[string]any{
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
			return
		}

		checkTs = parentTs
		epochsChecked++
	}

	log.Printf("  [heavy-compute] OK: verified %d epochs on %s", epochsChecked, nodeName)

	assert.Sometimes(epochsChecked > 0, "heavy_compute_verified", map[string]any{
		"node":           nodeName,
		"epochs_checked": epochsChecked,
	})
}

// ===========================================================================
// DoChainMonitor (Consensus & Node Health)
//
// Six sub-checks picked randomly per invocation:
//   1. Tipset consensus at a finalized height
//   2. Height progression (all nodes advancing)
//   3. Peer count (all nodes have peers)
//   4. Chain head comparison (finalized tipsets)
//   5. State root comparison at a finalized height
//   6. State audit (state roots + msg/receipt verification)
//
// State-sensitive checks (1, 4, 5, 6) use ChainGetFinalizedTipSet so they
// are safe during partition → reorg chaos.
// ===========================================================================

const (
	consensusWalkEpochs = 5
	finalizedMinHeight  = 5 // skip checks until finalized tipset is past this
)

func DoChainMonitor() {
	subCheck := rngIntn(6)
	checkNames := []string{"tipset-consensus", "height-progression", "peer-count", "head-comparison", "state-root-comparison", "state-audit"}
	log.Printf("  [chain-monitor] sub-check: %s", checkNames[subCheck])

	switch subCheck {
	case 0:
		doTipsetConsensus()
	case 1:
		doHeightProgression()
	case 2:
		doPeerCount()
	case 3:
		doHeadComparison()
	case 4:
		doStateRootComparison()
	case 5:
		doStateAudit()
	}
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
func doTipsetConsensus() {
	if len(nodeKeys) < 2 {
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

	if errs == len(nodeKeys) {
		return // all failed, can't assert
	}

	consensusReached := len(tipsetKeys) == 1 && errs == 0

	assert.Always(consensusReached, "tipset_consensus", map[string]any{
		"height":         checkHeight,
		"finalized_at":   finalizedHeight,
		"tipset_keys":    tipsetKeys,
		"unique_tipsets": len(tipsetKeys),
		"nodes_checked":  len(nodeKeys),
		"errors":         errs,
	})

	assert.Sometimes(consensusReached, "tipset_consensus_verified", map[string]any{
		"height": checkHeight,
	})
}

// doHeightProgression checks that all nodes are advancing.
// Ported from node-health.go CheckHeightProgression.
func doHeightProgression() {
	heights := make(map[string]abi.ChainEpoch)
	for _, name := range nodeKeys {
		head, err := nodes[name].ChainHead(ctx)
		if err != nil {
			log.Printf("[chain-monitor] ChainHead failed for %s: %v", name, err)
			continue
		}
		heights[name] = head.Height()
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

	// Nodes shouldn't be too far apart (>10 epochs suggests a problem)
	spread := maxH - minH
	acceptable := spread <= 10

	assert.Always(acceptable, "node_height_spread_acceptable", map[string]any{
		"heights": heights,
		"spread":  spread,
		"min":     minH,
		"max":     maxH,
	})

	// All nodes should be past genesis
	assert.Sometimes(minH > 0, "all_nodes_past_genesis", map[string]any{
		"min_height": minH,
	})
}

// doPeerCount checks that all nodes have peers.
// Ported from node-health.go CheckPeerCount.
func doPeerCount() {
	for _, name := range nodeKeys {
		peers, err := nodes[name].NetPeers(ctx)
		if err != nil {
			log.Printf("[chain-monitor] NetPeers failed for %s: %v", name, err)
			continue
		}

		peerCount := len(peers)

		assert.Always(peerCount > 0, "node_has_peers", map[string]any{
			"node":       name,
			"node_type":  nodeType(name),
			"peer_count": peerCount,
		})

		assert.Sometimes(peerCount > 0, "peer_connectivity_verified", map[string]any{
			"node":       name,
			"peer_count": peerCount,
		})
	}
}

// doHeadComparison queries ChainHead from all nodes and compares.
// Simpler than full tipset consensus — just checks heads are close.
func doHeadComparison() {
	if len(nodeKeys) < 2 {
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

		assert.Always(allMatch, "same_height_same_tipset", map[string]any{
			"height":     height,
			"nodes":      len(group),
			"keys_match": allMatch,
		})
	}
}

// doStateRootComparison compares parent state roots across all nodes at a finalized height.
// Catches state divergence. Uses finalized tipset so partitions don't cause false positives.
func doStateRootComparison() {
	if len(nodeKeys) < 2 {
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

	assert.Always(statesMatch, "cross_node_state_consistent", map[string]any{
		"height":        checkHeight,
		"finalized_at":  finalizedHeight,
		"state_roots":   stateRoots,
		"unique_states": len(stateRoots),
		"nodes_checked": len(nodeKeys),
	})

	if statesMatch {
		log.Printf("  [chain-monitor] OK: all %d nodes agree at height %d (finalized=%d)", len(nodeKeys), checkHeight, finalizedHeight)
		assert.Sometimes(true, "shared_state_verified", map[string]any{
			"height": checkHeight,
		})
	} else {
		log.Printf("  [chain-monitor] DIVERGENCE at height %d: %v", checkHeight, stateRoots)
	}
}

// doStateAudit compares state roots, parent messages, and parent receipts
// across nodes at a finalized height. Catches non-determinism in FVM execution
// that would cause consensus splits (the Dec 2020 chain halt bug class).
func doStateAudit() {
	if len(nodeKeys) < 2 {
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

	assert.Always(rootsMatch, "state_root_post_fvm_consistent", map[string]any{
		"height":        checkHeight,
		"finalized_at":  finalizedHeight,
		"unique_states": len(stateRoots),
		"state_roots":   stateRoots,
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
		assert.Always(msgsMatch, "parent_messages_cross_node_match", map[string]any{
			"height":  checkHeight,
			"block":   blkCid.String()[:16],
			"count_a": len(msgsA),
			"count_b": len(msgsB),
		})

		receiptsMatch := len(receiptsA) == len(receiptsB)
		assert.Always(receiptsMatch, "parent_receipts_cross_node_match", map[string]any{
			"height":  checkHeight,
			"block":   blkCid.String()[:16],
			"count_a": len(receiptsA),
			"count_b": len(receiptsB),
		})

		msgReceiptMatch := len(msgsA) == len(receiptsA)
		assert.Always(msgReceiptMatch, "message_receipt_count_match", map[string]any{
			"height":   checkHeight,
			"block":    blkCid.String()[:16],
			"msgs":     len(msgsA),
			"receipts": len(receiptsA),
		})

		if !msgsMatch || !receiptsMatch || !msgReceiptMatch {
			log.Printf("[chain-monitor] MESSAGE/RECEIPT MISMATCH at height %d block %s",
				checkHeight, blkCid.String()[:16])
		}
	}

	log.Printf("  [chain-monitor] OK: state-audit height %d, roots match, msgs/receipts consistent", checkHeight)

	assert.Sometimes(true, "state_audit_verified", map[string]any{
		"height": checkHeight,
	})
}
