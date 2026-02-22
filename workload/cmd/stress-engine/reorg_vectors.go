package main

import (
	"log"
	"time"

	"github.com/antithesishq/antithesis-sdk-go/assert"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/libp2p/go-libp2p/core/peer"
)

// ===========================================================================
// Vector 11: DoReorgChaos (Consensus Integrity — Reorg Simulation)
//
// Induces rapid, shallow forks by repeatedly isolating a node from the
// network, letting the main partition mine 1-3 blocks, then reconnecting.
// This stresses:
//   - Chain revert/reorg logic in the FVM and ChainStore
//   - SplitStore (hot/cold storage) canonical head tracking
//   - State tree rollback and re-application
//   - Gossip protocol recovery after partition heal
//
// Pattern: Split → Mine 1-3 blocks → Heal → repeat N times → Verify
//
// Security value: Tests database transactionality. Bugs here lead to
// "State Divergence" — the most severe consensus failure class.
// ===========================================================================

const (
	reorgMaxCyclesPerCall = 10               // max rapid partition cycles per invocation
	reorgConvergeWait     = 90 * time.Second // wait for sync after all cycles
	reorgEpochTimeout     = 30 * time.Second // max wait for epoch advance
	reorgPostHealPause    = 2 * time.Second  // brief pause after reconnect
	reorgReconnectPause   = 3 * time.Second  // wait after emergency reconnect
	reorgFallbackBlock    = 6 * time.Second  // fallback per-block sleep
)

func DoReorgChaos() {
	if len(nodeKeys) < 2 {
		return
	}

	// Pick a victim node to isolate
	victimName := rngChoice(nodeKeys)
	victim := nodes[victimName]

	// Random number of rapid split-heal cycles: 1-10
	numCycles := rngIntn(reorgMaxCyclesPerCall) + 1

	log.Printf("[reorg-chaos] starting %d rapid partition cycles, victim=%s", numCycles, victimName)

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

		// === PARTITION: disconnect victim from all peers ===
		disconnected := 0
		for _, p := range peers {
			if err := victim.NetDisconnect(ctx, p.ID); err == nil {
				disconnected++
			}
		}

		// Verify isolation
		postPeers, _ := victim.NetPeers(ctx)
		isolated := len(postPeers) == 0

		assert.Sometimes(isolated, "reorg_node_isolated", map[string]any{
			"victim":       victimName,
			"victim_type":  nodeType(victimName),
			"cycle":        cycle + 1,
			"total":        numCycles,
			"pre_peers":    len(peers),
			"disconnected": disconnected,
			"post_peers":   len(postPeers),
		})

		log.Printf("[reorg-chaos] cycle %d/%d: SPLIT %s (disconnected %d/%d, isolated=%v)",
			cycle+1, numCycles, victimName, disconnected, len(peers), isolated)

		// === MINE: wait for 1-3 epochs on the main partition ===
		blocksToWait := rngIntn(3) + 1
		waitForEpochsOnOther(victimName, blocksToWait)

		// === HEAL: reconnect victim to all saved peers + known nodes ===
		reconnected := 0
		for _, p := range savedPeers {
			if err := victim.NetConnect(ctx, p); err == nil {
				reconnected++
			}
		}
		// Also try known node addresses as fallback
		for _, p := range knownPeers {
			victim.NetConnect(ctx, p) // best-effort
		}

		log.Printf("[reorg-chaos] cycle %d/%d: HEAL %s (reconnected %d/%d)",
			cycle+1, numCycles, victimName, reconnected, len(savedPeers))

		// Brief pause for sync to begin before next cycle
		time.Sleep(reorgPostHealPause)

		successfulCycles++
	}

	if successfulCycles == 0 {
		return
	}

	assert.Sometimes(successfulCycles > 0, "reorg_chaos_executed", map[string]any{
		"victim":    victimName,
		"cycles":    successfulCycles,
		"requested": numCycles,
	})

	// Wait for full convergence after all cycles
	log.Printf("[reorg-chaos] waiting for convergence after %d cycles...", successfulCycles)
	time.Sleep(reorgConvergeWait)

	verifyPostReorgState(victimName, successfulCycles)
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

		assert.Always(hasPeers, "post_reorg_network_healed", map[string]any{
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
	for _, name := range nodeKeys {
		finTs, err := nodes[name].ChainGetFinalizedTipSet(ctx)
		if err != nil {
			log.Printf("[reorg-chaos] ChainGetFinalizedTipSet failed for %s: %v", name, err)
			return
		}
		ts, err := nodes[name].ChainGetTipSetByHeight(ctx, checkHeight, finTs.Key())
		if err != nil {
			log.Printf("[reorg-chaos] ChainGetTipSetByHeight(%d) failed for %s: %v", checkHeight, name, err)
			return
		}
		root := ts.ParentState().String()
		stateRoots[root] = append(stateRoots[root], name)
	}

	statesMatch := len(stateRoots) == 1

	assert.Always(statesMatch, "post_reorg_state_consistent", map[string]any{
		"victim":        victimName,
		"height":        checkHeight,
		"finalized_at":  finalizedHeight,
		"unique_states": len(stateRoots),
		"state_roots":   stateRoots,
		"cycles":        cycles,
	})

	// Check 3: Height spread — nodes shouldn't be too far apart after convergence
	heights := make(map[string]abi.ChainEpoch)
	for _, name := range nodeKeys {
		head, err := nodes[name].ChainHead(ctx)
		if err == nil {
			heights[name] = head.Height()
		}
	}

	if len(heights) < 2 {
		return
	}

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

	spread := maxH - minH
	acceptable := spread <= 10

	assert.Always(acceptable, "post_reorg_height_spread_ok", map[string]any{
		"victim":  victimName,
		"heights": heights,
		"spread":  spread,
		"cycles":  cycles,
	})

	// Liveness: full convergence achieved
	converged := statesMatch && acceptable

	assert.Sometimes(converged, "reorg_convergence_achieved", map[string]any{
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
			cycles, statesMatch, spread, heights)
	}
}
