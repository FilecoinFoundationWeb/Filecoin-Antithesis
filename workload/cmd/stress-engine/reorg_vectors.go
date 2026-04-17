package main

import (
	"log"
	"time"

	"github.com/antithesishq/antithesis-sdk-go/assert"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
	"github.com/libp2p/go-libp2p/core/peer"
)

// ===========================================================================
// DoReorgChaos (Consensus Integrity — Reorg Simulation)
//
// Induces rapid, shallow forks by repeatedly isolating a node from the
// network, letting the main partition mine 1-3 blocks, then reconnecting.
// This stresses:
//   - Chain revert/reorg logic in the FVM and ChainStore
//   - SplitStore (hot/cold storage) canonical head tracking
//   - State tree rollback and re-application
//   - Gossip protocol recovery after partition heal
//   - F3 quorum behavior under power-aware partitions
//
// Victim selection is power-aware when F3 power table is available:
// targets the biggest or smallest miner's node to test F3 quorum edges.
// Falls back to random victim when power table is unavailable.
//
// Pattern: Split → Mine 1-3 blocks → Heal → repeat N times → Verify
// ===========================================================================

const (
	reorgMaxCyclesPerCall  = 10                 // max rapid partition cycles per invocation
	reorgConvergeTimeout   = 3 * time.Minute    // max wait for convergence after all cycles
	reorgConvergePollRate  = 3 * time.Second    // poll interval during convergence wait
	reorgConvergeMaxSpread = abi.ChainEpoch(10) // max allowed spread between nodes
	reorgEpochTimeout      = 30 * time.Second   // max wait for epoch advance
	reorgPostHealPause     = 2 * time.Second    // brief pause after reconnect
	reorgReconnectPause    = 3 * time.Second    // wait after emergency reconnect
	reorgFallbackBlock     = 9 * time.Second    // fallback per-block sleep
)

// pickReorgVictim selects a victim node for partition. When the F3 power table
// is available, it biases toward the biggest or smallest miner's node (50/50).
// Falls back to uniform random selection otherwise.
func pickReorgVictim() (victimName string, powerPct float64) {
	lotusNode, _ := pickLotusNode()
	if lotusNode != nil {
		table := getF3PowerTable(lotusNode)
		if len(table) >= 2 {
			var target minerPowerInfo
			if focCfg != nil {
				// FOC active: protect lotus0 (Curio's backend).
				// Only target non-lotus0 miners for benign reorgs.
				var candidates []minerPowerInfo
				for _, m := range table {
					if minerToNodeName(m.addr) != "lotus0" {
						candidates = append(candidates, m)
					}
				}
				if len(candidates) > 0 {
					target = candidates[rngIntn(len(candidates))]
				} else {
					target = table[len(table)-1]
				}
			} else if rngIntn(2) == 0 {
				target = table[0] // biggest
			} else {
				target = table[len(table)-1] // smallest
			}
			name := minerToNodeName(target.addr)
			if name != "" {
				return name, target.pct
			}
		}
	}
	return rngChoice(nodeKeys), 0
}

func DoReorgChaos() {
	// Skip if n-split consensus test already has a partition active
	if partitionActive.Load() {
		debugLog("[reorg-chaos] skipping — partition already active")
		return
	}
	if len(nodeKeys) < 2 {
		return
	}

	// Pick a victim node — power-aware when possible
	victimName, victimPowerPct := pickReorgVictim()
	victim := nodes[victimName]

	// Random number of rapid split-heal cycles.
	// FOC: lighter reorgs (1-3) to avoid disrupting Curio lifecycle.
	maxCycles := reorgMaxCyclesPerCall
	if focCfg != nil {
		maxCycles = 3
	}
	numCycles := rngIntn(maxCycles) + 1

	if victimPowerPct > 0 {
		log.Printf("[reorg-chaos] starting %d rapid partition cycles, victim=%s (%.1f%% power)", numCycles, victimName, victimPowerPct)
	} else {
		log.Printf("[reorg-chaos] starting %d rapid partition cycles, victim=%s", numCycles, victimName)
	}

	// Snapshot F3 instance before partition cycles
	lotusNode, _ := pickLotusNode()
	var preF3Inst uint64
	var f3ok bool
	if lotusNode != nil {
		preF3Inst, f3ok = getF3Instance(lotusNode)
	}

	// Collect known node addresses for reliable reconnection
	knownPeers := collectNodeAddrInfos(victimName)

	successfulCycles := 0

	for cycle := 0; cycle < numCycles; cycle++ {
		// Get current peers of the victim
		peers, err := victim.NetPeers(ctx)
		if err != nil {
			log.Printf("[reorg-chaos] cycle %d: NetPeers failed: %v", cycle+1, err)
			break
		}
		if len(peers) == 0 {
			log.Printf("[reorg-chaos] cycle %d: victim has no peers, reconnecting...", cycle+1)
			for _, p := range knownPeers {
				victim.NetConnect(ctx, p)
			}
			time.Sleep(reorgReconnectPause)
			continue
		}

		// Save peer infos for reconnection after partition
		savedPeers := make([]peer.AddrInfo, len(peers))
		copy(savedPeers, peers)

		// === PARTITION: disconnect + block victim from all peers ===
		partitionActive.Store(true)
		disconnected := 0
		blockPeerIDs := make([]peer.ID, 0, len(peers))
		for _, p := range peers {
			if err := victim.NetDisconnect(ctx, p.ID); err == nil {
				disconnected++
			}
			blockPeerIDs = append(blockPeerIDs, p.ID)
		}

		// Block on victim so peers can't reconnect
		if err := victim.NetBlockAdd(ctx, api.NetBlockList{Peers: blockPeerIDs}); err != nil {
			log.Printf("[reorg-chaos] cycle %d: NetBlockAdd on victim failed: %v", cycle+1, err)
		}

		// Block victim on each other node (both directions)
		victimAddrInfo, _ := victim.NetAddrsListen(ctx)
		for _, p := range knownPeers {
			// knownPeers excludes victim, so these are all other nodes
			for _, name := range nodeKeys {
				if name == victimName {
					continue
				}
				ai, err := nodes[name].NetAddrsListen(ctx)
				if err == nil && ai.ID == p.ID {
					nodes[name].NetBlockAdd(ctx, api.NetBlockList{Peers: []peer.ID{victimAddrInfo.ID}})
					nodes[name].NetDisconnect(ctx, victimAddrInfo.ID)
					break
				}
			}
		}

		// Verify isolation
		postPeers, _ := victim.NetPeers(ctx)
		isolated := len(postPeers) == 0

		log.Printf("[reorg-chaos] cycle %d/%d: SPLIT %s (disconnected %d/%d, blocked=%d, isolated=%v)",
			cycle+1, numCycles, victimName, disconnected, len(peers), len(blockPeerIDs), isolated)

		// === MINE: wait for 1-3 epochs on the main partition ===
		blocksToWait := rngIntn(3) + 1
		waitForEpochsOnOther(victimName, blocksToWait)

		// === HEAL: unblock + reconnect victim ===
		// Remove blocks on victim
		victim.NetBlockRemove(ctx, api.NetBlockList{Peers: blockPeerIDs})
		// Remove victim block on all other nodes
		for _, name := range nodeKeys {
			if name == victimName {
				continue
			}
			nodes[name].NetBlockRemove(ctx, api.NetBlockList{Peers: []peer.ID{victimAddrInfo.ID}})
		}

		// Reconnect
		reconnected := 0
		for _, p := range savedPeers {
			if err := victim.NetConnect(ctx, p); err == nil {
				reconnected++
			}
		}
		for _, p := range knownPeers {
			victim.NetConnect(ctx, p)
		}

		partitionActive.Store(false)
		log.Printf("[reorg-chaos] cycle %d/%d: HEAL %s (reconnected %d/%d)",
			cycle+1, numCycles, victimName, reconnected, len(savedPeers))

		// Brief pause for sync to begin before next cycle
		time.Sleep(reorgPostHealPause)

		successfulCycles++
	}

	if successfulCycles == 0 {
		return
	}

	// Full mesh reconnect: Antithesis fault injection can sever connections
	// between ANY nodes (not just the victim), so after all cycles we must
	// ensure every node is connected to every other node.
	ensureFullMesh()

	// Wait for convergence by polling finalized heights
	log.Printf("[reorg-chaos] waiting for convergence after %d cycles...", successfulCycles)
	converged := waitForConvergence(victimName)

	assert.Sometimes(converged, "Nodes converged within timeout after reorg", map[string]any{
		"victim":  victimName,
		"cycles":  successfulCycles,
		"timeout": reorgConvergeTimeout.String(),
	})

	if converged {
		verifyPostReorgState(victimName, successfulCycles)
		// FOC: verify proofset survived the reorg
		if focCfg != nil {
			verifyFOCStateAfterReorg()
		}
	}

	// Check F3 progress after reorg cycles
	if f3ok && lotusNode != nil {
		advanced, postF3Inst := checkF3Advancing(lotusNode, preF3Inst, 2*time.Minute)

		assert.Sometimes(advanced, "F3 advances after reorg cycles", map[string]any{
			"victim":        victimName,
			"power_pct":     victimPowerPct,
			"cycles":        successfulCycles,
			"pre_instance":  preF3Inst,
			"post_instance": postF3Inst,
			"converged":     converged,
		})

		if advanced {
			debugLog("[reorg-chaos] F3 advanced %d→%d after %d cycles", preF3Inst, postF3Inst, successfulCycles)
		}
	}
}

// waitForConvergence polls all nodes' finalized tipset heights until the
// spread is within reorgConvergeMaxSpread or the timeout expires.
// Tolerates individual node errors — checks convergence among responding
// nodes (requires at least 2). Tracks whether the slowest responding node
// is making progress to avoid premature timeout during sync.
func waitForConvergence(victimName string) bool {
	deadline := time.Now().Add(reorgConvergeTimeout)
	prevMinH := abi.ChainEpoch(0)
	stallCount := 0
	const maxStallPolls = 10 // 10 * 3s = 30s of no progress before giving up

	for time.Now().Before(deadline) {
		var minH, maxH abi.ChainEpoch
		responded := 0
		for _, name := range nodeKeys {
			ts, err := nodes[name].ChainGetFinalizedTipSet(ctx)
			if err != nil {
				debugLog("[reorg-chaos] ChainGetFinalizedTipSet failed for %s: %v", name, err)
				continue
			}
			responded++
			if responded == 1 {
				minH, maxH = ts.Height(), ts.Height()
			}
			if ts.Height() < minH {
				minH = ts.Height()
			}
			if ts.Height() > maxH {
				maxH = ts.Height()
			}
		}

		if responded < 2 {
			debugLog("[reorg-chaos] only %d nodes responded, retrying...", responded)
			time.Sleep(reorgConvergePollRate)
			continue
		}

		if (maxH - minH) <= reorgConvergeMaxSpread {
			// Verify peer connectivity — spread can look OK while nodes
			// are isolated (each advancing their own fork independently).
			isolated := 0
			for _, name := range nodeKeys {
				peers, err := nodes[name].NetPeers(ctx)
				if err != nil || len(peers) == 0 {
					isolated++
				}
			}
			if isolated > 0 {
				log.Printf("[reorg-chaos] spread OK but %d/%d nodes have no peers, re-meshing...",
					isolated, len(nodeKeys))
				ensureFullMesh()
				time.Sleep(reorgConvergePollRate)
				continue
			}
			log.Printf("[reorg-chaos] converged: spread=%d epochs, %d/%d nodes responded (victim=%s)",
				maxH-minH, responded, len(nodeKeys), victimName)
			return true
		}

		// Track progress: if minH advanced, nodes are syncing — reset stall counter
		if minH > prevMinH {
			stallCount = 0
			prevMinH = minH
		} else {
			stallCount++
		}

		if stallCount >= maxStallPolls {
			log.Printf("[reorg-chaos] convergence stalled: no progress for %d polls (victim=%s, spread=%d, responded=%d/%d)",
				stallCount, victimName, maxH-minH, responded, len(nodeKeys))
			return false
		}

		log.Printf("[reorg-chaos] waiting for convergence: spread=%d, minH=%d, responded=%d/%d (victim=%s)",
			maxH-minH, minH, responded, len(nodeKeys), victimName)
		time.Sleep(reorgConvergePollRate)
	}
	log.Printf("[reorg-chaos] convergence timeout after %s", reorgConvergeTimeout)
	return false
}

// ensureFullMesh connects every node to every other node and clears all
// blocklists. Antithesis fault injection can sever connections between any
// pair of nodes at any time, so after reorg cycles we must restore the
// full mesh rather than only reconnecting the victim.
func ensureFullMesh() {
	// Step 1: Clear all blocklists on all nodes
	for _, name := range nodeKeys {
		bl, err := nodes[name].NetBlockList(ctx)
		if err != nil || (len(bl.Peers) == 0 && len(bl.IPAddrs) == 0 && len(bl.IPSubnets) == 0) {
			continue
		}
		if err := nodes[name].NetBlockRemove(ctx, bl); err != nil {
			debugLog("[reorg-chaos] NetBlockRemove on %s failed: %v", name, err)
		}
	}

	// Step 2: Collect all node addresses
	allAddrs := collectNodeAddrInfos("")

	// Step 3: Connect every node to every other node
	for _, name := range nodeKeys {
		for _, addr := range allAddrs {
			nodes[name].NetConnect(ctx, addr)
		}
	}
}

// collectNodeAddrInfos gets the listening addresses of all known nodes
// except the excluded one. Used for reliable reconnection after partition.
func collectNodeAddrInfos(excludeNode string) []peer.AddrInfo {
	var infos []peer.AddrInfo
	for _, name := range nodeKeys {
		if name == excludeNode {
			continue
		}
		addrInfo, err := nodes[name].NetAddrsListen(ctx)
		if err != nil {
			log.Printf("[reorg-chaos] NetAddrsListen failed for %s: %v", name, err)
			continue
		}
		infos = append(infos, addrInfo)
	}
	return infos
}

// waitForEpochsOnOther waits for N epochs to advance on a non-victim node.
// This ensures blocks are actually mined during the partition window.
// Falls back to time-based wait if monitoring fails.
func waitForEpochsOnOther(excludeNode string, n int) {
	var watchName string
	for _, name := range nodeKeys {
		if name != excludeNode {
			watchName = name

			break
		}
	}
	if watchName == "" {
		time.Sleep(time.Duration(n) * reorgFallbackBlock)
		return
	}

	startHead, err := nodes[watchName].ChainHead(ctx)
	if err != nil {
		time.Sleep(time.Duration(n) * reorgFallbackBlock)
		return
	}
	targetHeight := startHead.Height() + abi.ChainEpoch(n)

	deadline := time.After(reorgEpochTimeout)
	for {
		select {
		case <-deadline:
			log.Printf("[reorg-chaos] epoch wait timed out (watching=%s, target=%d)", watchName, targetHeight)
			return
		default:
			head, err := nodes[watchName].ChainHead(ctx)
			if err == nil && head.Height() >= targetHeight {
				return
			}
			time.Sleep(time.Second)
		}
	}
}

// verifyPostReorgState runs convergence checks after reorg cycles complete.
// Verifies: network healed, finalized state consistent, no zombie state.
func verifyPostReorgState(victimName string, cycles int) {
	// Check 1: Network healed — all nodes have peers
	for _, name := range nodeKeys {
		peers, err := nodes[name].NetPeers(ctx)
		if err != nil {
			continue
		}
		hasPeers := len(peers) > 0

		assert.Sometimes(hasPeers, "Network connectivity restored after reorg", map[string]any{
			"node":       name,
			"node_type":  nodeType(name),
			"victim":     victimName,
			"peer_count": len(peers),
			"cycles":     cycles,
		})

		if !hasPeers {
			log.Printf("[reorg-chaos] WARNING: %s has no peers after heal!", name)
		}
	}

	// Check 2: Finalized state consistency — no zombie state
	finalizedHeight, _ := getFinalizedHeight()
	if finalizedHeight < finalizedMinHeight {
		log.Printf("[reorg-chaos] finalized height %d too low for state check", finalizedHeight)
		return
	}

	checkHeight := abi.ChainEpoch(rngIntn(int(finalizedHeight)) + 1)

	stateRoots := make(map[string][]string)
	finalizedHeights := make(map[string]abi.ChainEpoch)
	for _, name := range nodeKeys {
		finTs, err := nodes[name].ChainGetFinalizedTipSet(ctx)
		if err != nil {
			log.Printf("[reorg-chaos] ChainGetFinalizedTipSet failed for %s: %v", name, err)
			return
		}
		finalizedHeights[name] = finTs.Height()
		ts, err := nodes[name].ChainGetTipSetByHeight(ctx, checkHeight, finTs.Key())
		if err != nil {
			log.Printf("[reorg-chaos] ChainGetTipSetByHeight(%d) failed for %s: %v", checkHeight, name, err)
			return
		}
		root := ts.ParentState().String()
		stateRoots[root] = append(stateRoots[root], name)
	}

	statesMatch := len(stateRoots) == 1

	// Sometimes: post-reorg state comparison with shallow EC finality (head-20)
	// may catch nodes still on different fork branches during convergence.
	assert.Sometimes(statesMatch, "Chain state is consistent after reorg", map[string]any{
		"victim":        victimName,
		"height":        checkHeight,
		"finalized_at":  finalizedHeight,
		"unique_states": len(stateRoots),
		"state_roots":   stateRoots,
		"cycles":        cycles,
	})

	// Check 3: Finalized height spread — nodes shouldn't be too far apart after convergence.
	// Uses finalizedHeights collected above to avoid false positives from nodes legitimately
	// lagging on live block processing (e.g. forest catching up after partition heal).
	if len(finalizedHeights) < 2 {
		return
	}

	var minH, maxH abi.ChainEpoch
	first := true
	for _, h := range finalizedHeights {
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

	spread := maxH - minH
	acceptable := spread <= 10

	assert.Sometimes(acceptable, "Node heights within acceptable range after reorg", map[string]any{
		"victim":  victimName,
		"heights": finalizedHeights,
		"spread":  spread,
		"cycles":  cycles,
	})

	// Liveness: full convergence achieved
	converged := statesMatch && acceptable

	assert.Sometimes(converged, "Nodes converged after reorg", map[string]any{
		"victim":       victimName,
		"cycles":       cycles,
		"states_match": statesMatch,
		"spread":       spread,
	})

	if converged {
		log.Printf("[reorg-chaos] OK: convergence verified after %d cycles (victim=%s, height=%d, spread=%d)",
			cycles, victimName, checkHeight, spread)
	} else {
		log.Printf("[reorg-chaos] DIVERGENCE after %d cycles: states_match=%v spread=%d heights=%v",
			cycles, statesMatch, spread, finalizedHeights)
	}
}
