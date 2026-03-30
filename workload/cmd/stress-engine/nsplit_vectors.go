package main

import (
	"log"
	"sync"

	"github.com/antithesishq/antithesis-sdk-go/assert"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p/core/peer"
)

// ===========================================================================
// N-Split Attack Vectors (EC/F3 Security Threshold Testing)
//
//   DoNetworkBisection     — creates partition topologies (star, bilateral, isolation)
//   DoNetworkHeal          — reconnects all nodes to full mesh
//   DoDoubleSpendDuringFork — opportunistically injects double-spend when fork exists
//   DoDoubleSpendVerify    — checks pending double-spends for safety violations
//
// ===========================================================================

const (
	// nsplitConvergenceBuffer is how many epochs must pass after a double-spend
	// injection before we verify the outcome.
	nsplitConvergenceBuffer = 50

	// nsplitMaxPending limits memory for tracked double-spends.
	nsplitMaxPending = 50

	// EC security threshold — adversary above this can break EC (Wang 2023, m=5)
	ecThresholdPct = 20.0

	// F3 quorum threshold — need > 67% honest power for F3 to finalize
	f3QuorumPct = 67.0
)

// pendingDoubleSpend tracks a pair of conflicting transactions for later verification.
type pendingDoubleSpend struct {
	fromAddr   address.Address
	nonce      uint64
	cidA       cid.Cid
	cidB       cid.Cid
	nodeA      string
	nodeB      string
	injectedAt abi.ChainEpoch // chain head when injected
}

var (
	pendingDoubleSpends   []pendingDoubleSpend
	pendingDoubleSpendsMu sync.Mutex
)

// ===========================================================================
// DoNetworkBisection — Create Partition Topologies
// ===========================================================================

// DoNetworkBisection picks a random partition strategy and executes ONE
// partition step. It does not heal — that's DoNetworkHeal's job.
// Over many deck iterations, this creates varied network topologies
// that Antithesis explores.
func DoNetworkBisection() {
	if len(nodeKeys) < 2 {
		return
	}

	strategy := rngIntn(100)
	switch {
	case strategy < 40:
		doStarSplit()
	case strategy < 80:
		doBilateralSplit()
	default:
		doFullIsolation()
	}
}

// doStarSplit picks a hub node (power-aware) and disconnects all non-hub
// nodes from each other. The hub stays connected to everyone.
// This simulates the n-split attack topology from Wang et al. 2023.
func doStarSplit() {
	// Pick hub (adversary role) based on power table
	lotusNode, _ := pickLotusNode()
	if lotusNode == nil {
		return
	}

	table := getF3PowerTable(lotusNode)
	if len(table) < 2 {
		return
	}

	// Pick a random miner as hub
	hubMiner := table[rngIntn(len(table))]
	hubName := minerToNodeName(hubMiner.addr)
	if hubName == "" {
		return
	}

	// Get the hub's peer ID so we can identify it
	hubAddrInfo, err := nodes[hubName].NetAddrsListen(ctx)
	if err != nil {
		return
	}
	hubPeerID := hubAddrInfo.ID

	// For each non-hub node, disconnect from all other non-hub nodes
	// (keep connection to hub)
	totalDisconnected := 0
	honestNodes := []string{}
	honestPowers := map[string]float64{}

	for _, m := range table {
		name := minerToNodeName(m.addr)
		if name == "" || name == hubName {
			continue
		}
		honestNodes = append(honestNodes, name)
		honestPowers[name] = m.pct
	}

	for _, name := range honestNodes {
		peers, err := nodes[name].NetPeers(ctx)
		if err != nil {
			continue
		}
		for _, p := range peers {
			// Don't disconnect from the hub
			if p.ID == hubPeerID {
				continue
			}
			// Disconnect from other honest nodes
			if err := nodes[name].NetDisconnect(ctx, p.ID); err == nil {
				totalDisconnected++
			}
		}
	}

	// Compute honest power total
	totalHonest := 0.0
	for _, pct := range honestPowers {
		totalHonest += pct
	}

	// Log expected impact
	log.Printf("[nsplit] === bisection: star ===")
	log.Printf("[nsplit] hub: %s (%s, %.1f%% power)", hubName, hubMiner.addr, hubMiner.pct)
	for _, name := range honestNodes {
		log.Printf("[nsplit]   isolated: %s (%.1f%%)", name, honestPowers[name])
	}
	log.Printf("[nsplit] disconnected %d honest-to-honest connections", totalDisconnected)

	// Log security analysis
	ecVulnerable := hubMiner.pct >= ecThresholdPct
	f3HasQuorum := totalHonest > f3QuorumPct

	var expected string
	switch {
	case hubMiner.pct < ecThresholdPct:
		expected = "EC safe, F3 safe — no attack should succeed"
	case hubMiner.pct < 100-f3QuorumPct:
		expected = "EC VULNERABLE, F3 safe — forks expected but F3 protects"
	case hubMiner.pct < 50:
		expected = "EC VULNERABLE, F3 VULNERABLE — both may fail"
	default:
		expected = "EC BROKEN, F3 BROKEN — catastrophic consensus failure expected"
	}
	log.Printf("[nsplit] EC threshold: vulnerable=%v (%.1f%% vs 20%%)", ecVulnerable, hubMiner.pct)
	log.Printf("[nsplit] F3 quorum: %v (%.1f%% honest > 67%%)", f3HasQuorum, totalHonest)
	log.Printf("[nsplit] expected: %s", expected)

	assert.Sometimes(totalDisconnected > 0, "N-split star topology created", map[string]any{
		"hub":              hubName,
		"hub_power":        hubMiner.pct,
		"honest_nodes":     honestNodes,
		"total_honest_pct": totalHonest,
		"disconnected":     totalDisconnected,
		"ec_vulnerable":    ecVulnerable,
		"f3_has_quorum":    f3HasQuorum,
	})
}

// doBilateralSplit disconnects two random nodes from each other.
// Creates partial mesh degradation.
func doBilateralSplit() {
	nameA, nameB, nodeA, nodeB := pickTwoDistinctNodes()
	if nodeA == nil {
		return
	}

	// Get B's peer ID
	addrInfoB, err := nodeB.NetAddrsListen(ctx)
	if err != nil {
		return
	}

	// Disconnect A from B
	disconnected := false
	peers, err := nodeA.NetPeers(ctx)
	if err != nil {
		return
	}
	for _, p := range peers {
		if p.ID == addrInfoB.ID {
			if err := nodeA.NetDisconnect(ctx, p.ID); err == nil {
				disconnected = true
			}
			break
		}
	}

	if disconnected {
		log.Printf("[nsplit] bilateral split: %s ↔ %s disconnected", nameA, nameB)
	}
}

// doFullIsolation disconnects one node from ALL peers.
// Simulates an adversary going private to build a competing chain.
func doFullIsolation() {
	// Power-aware: pick based on power table when available
	victimName, victimPower := pickReorgVictim()
	victim := nodes[victimName]

	peers, err := victim.NetPeers(ctx)
	if err != nil || len(peers) == 0 {
		return
	}

	disconnected := 0
	for _, p := range peers {
		if err := victim.NetDisconnect(ctx, p.ID); err == nil {
			disconnected++
		}
	}

	log.Printf("[nsplit] full isolation: %s (%.1f%% power) disconnected from %d peers",
		victimName, victimPower, disconnected)
}

// ===========================================================================
// DoNetworkHeal — Reconnect All Nodes
// ===========================================================================

// DoNetworkHeal reconnects all nodes to a full mesh.
func DoNetworkHeal() {
	if len(nodeKeys) < 2 {
		return
	}

	// Collect all node addresses
	allAddrs := collectNodeAddrInfos("")

	reconnected := 0
	for _, name := range nodeKeys {
		node := nodes[name]
		for _, addr := range allAddrs {
			if err := node.NetConnect(ctx, addr); err == nil {
				reconnected++
			}
		}
	}

	log.Printf("[nsplit] healed network: %d connections established", reconnected)

	assert.Sometimes(reconnected > 0, "Network heal reconnected nodes", map[string]any{
		"connections": reconnected,
		"nodes":       len(nodeKeys),
	})
}

// ===========================================================================
// DoDoubleSpendDuringFork — Opportunistic Double-Spend Injection
// ===========================================================================

// DoDoubleSpendDuringFork checks if a fork currently exists (nodes disagree
// on finalized tipset) and if so, injects conflicting transactions to the
// disagreeing nodes. This is opportunistic — it only fires when conditions
// are right.
func DoDoubleSpendDuringFork() {
	if len(nodeKeys) < 2 {
		return
	}

	// Quick fork check: compare finalized tipsets from two random nodes
	nameA, nameB, nodeA, nodeB := pickTwoDistinctNodes()
	if nodeA == nil {
		return
	}

	finA, errA := nodeA.ChainGetFinalizedTipSet(ctx)
	finB, errB := nodeB.ChainGetFinalizedTipSet(ctx)
	if errA != nil || errB != nil {
		return
	}

	// No fork — nodes agree
	if finA.Key() == finB.Key() {
		return
	}

	// Fork detected! Nodes disagree on finalized tipset.
	log.Printf("[nsplit] fork detected between %s (height=%d) and %s (height=%d) — injecting double-spend",
		nameA, finA.Height(), nameB, finB.Height())

	// Pick a wallet and create conflicting transactions
	fromAddr, fromKI := pickWallet()
	toAddrA, _ := pickWallet()
	toAddrB, _ := pickWallet()
	if fromAddr == toAddrA || fromAddr == toAddrB || toAddrA == toAddrB {
		return
	}

	currentNonce := nonces[fromAddr]

	// Tx A → nodeA, Tx B → nodeB (same nonce, different recipients)
	msgA := baseMsg(fromAddr, toAddrA, abi.NewTokenAmount(1))
	cidA, okA := pushMsgManualNonce(nodeA, msgA, fromKI, currentNonce, "nsplit-dspend-A")

	msgB := baseMsg(fromAddr, toAddrB, abi.NewTokenAmount(1))
	cidB, okB := pushMsgManualNonce(nodeB, msgB, fromKI, currentNonce, "nsplit-dspend-B")

	// Consume the nonce regardless
	nonces[fromAddr]++

	if !okA || !okB {
		debugLog("[nsplit] double-spend push failed: okA=%v okB=%v", okA, okB)
		return
	}

	// Get current chain head for timing
	head, err := nodeA.ChainHead(ctx)
	if err != nil {
		return
	}

	// Track for later verification
	pendingDoubleSpendsMu.Lock()
	if len(pendingDoubleSpends) >= nsplitMaxPending {
		pendingDoubleSpends = pendingDoubleSpends[1:] // evict oldest
	}
	pendingDoubleSpends = append(pendingDoubleSpends, pendingDoubleSpend{
		fromAddr:   fromAddr,
		nonce:      currentNonce,
		cidA:       cidA,
		cidB:       cidB,
		nodeA:      nameA,
		nodeB:      nameB,
		injectedAt: head.Height(),
	})
	pendingDoubleSpendsMu.Unlock()

	log.Printf("[nsplit] double-spend injected: from=%s nonce=%d cidA=%s→%s cidB=%s→%s",
		fromAddr, currentNonce, cidStr(cidA), nameA, cidStr(cidB), nameB)

	assert.Sometimes(true, "Double-spend injected during active fork", map[string]any{
		"from":   fromAddr.String(),
		"nonce":  currentNonce,
		"node_a": nameA,
		"node_b": nameB,
	})
}

// ===========================================================================
// DoDoubleSpendVerify — Post-Convergence Safety Check
// ===========================================================================

// DoDoubleSpendVerify checks pending double-spend records. For records where
// enough epochs have passed since injection, it verifies that at most one
// of the conflicting transactions was included on-chain.
func DoDoubleSpendVerify() {
	pendingDoubleSpendsMu.Lock()
	defer pendingDoubleSpendsMu.Unlock()

	if len(pendingDoubleSpends) == 0 {
		return
	}

	// Get current chain head from any node
	lotusNode, _ := pickLotusNode()
	if lotusNode == nil {
		return
	}
	head, err := lotusNode.ChainHead(ctx)
	if err != nil {
		return
	}

	remaining := pendingDoubleSpends[:0]

	for _, ds := range pendingDoubleSpends {
		epochsSince := head.Height() - ds.injectedAt

		// Not enough time — keep tracking
		if epochsSince < nsplitConvergenceBuffer {
			remaining = append(remaining, ds)
			continue
		}

		// Check if each tx landed
		resultA, _ := lotusNode.StateSearchMsg(ctx, types.EmptyTSK, ds.cidA, 200, true)
		resultB, _ := lotusNode.StateSearchMsg(ctx, types.EmptyTSK, ds.cidB, 200, true)

		landedA := resultA != nil
		landedB := resultB != nil
		landed := 0
		if landedA {
			landed++
		}
		if landedB {
			landed++
		}

		safe := landed <= 1

		assert.Always(safe, "Double-spend safety: at most one tx included", map[string]any{
			"from":          ds.fromAddr.String(),
			"nonce":         ds.nonce,
			"cid_a":         cidStr(ds.cidA),
			"cid_b":         cidStr(ds.cidB),
			"node_a":        ds.nodeA,
			"node_b":        ds.nodeB,
			"landed_a":      landedA,
			"landed_b":      landedB,
			"total_landed":  landed,
			"injected_at":   ds.injectedAt,
			"verified_at":   head.Height(),
			"epochs_waited": epochsSince,
		})

		if !safe {
			log.Printf("[nsplit] DOUBLE SPEND SUCCEEDED — CONSENSUS SAFETY VIOLATION: "+
				"from=%s nonce=%d both cidA=%s and cidB=%s included on-chain",
				ds.fromAddr, ds.nonce, cidStr(ds.cidA), cidStr(ds.cidB))
		} else {
			debugLog("[nsplit] double-spend verified safe: from=%s nonce=%d landed=%d (after %d epochs)",
				ds.fromAddr, ds.nonce, landed, epochsSince)
		}

		assert.Sometimes(landed >= 1, "Double-spend: at least one tx eventually included", map[string]any{
			"from":         ds.fromAddr.String(),
			"nonce":        ds.nonce,
			"total_landed": landed,
		})

		// Verified — don't keep
	}

	pendingDoubleSpends = remaining
}

// ===========================================================================
// Helpers
// ===========================================================================

// collectNodeAddrInfosAll returns addresses for ALL nodes (no exclusion).
func collectNodeAddrInfosAll() []peer.AddrInfo {
	return collectNodeAddrInfos("")
}
