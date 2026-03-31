package main

import (
	"log"
	"math/big"
	"sync"
	"time"

	"github.com/antithesishq/antithesis-sdk-go/assert"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/ipfs/go-cid"
)

// ===========================================================================
// N-Split Attack Vectors (EC/F3 Security Threshold Testing)
//
//   DoNetworkBisection     — creates partition topologies (fire-and-forget)
//   DoNetworkHeal          — reconnects all nodes to full mesh + verifies
//   DoAdversarialDuringFork — opportunistically injects double-spend when fork exists
//   DoAdversarialVerify    — checks pending double-spends for safety violations
//
// Design: DoNetworkBisection creates the partition and returns immediately,
// allowing the deck to keep spinning. Transfers, gas wars, deploys, and
// adversarial vectors all run while the partition is active. DoNetworkHeal
// reconnects nodes AND runs post-partition F3/EC verification.
// ===========================================================================

const (
	// nsplitConvergenceBuffer is how many epochs must pass after a double-spend
	// injection before we verify the outcome. Must exceed EC finality depth (20)
	// to ensure the chain has settled.
	nsplitConvergenceBuffer = 25

	// nsplitMaxPending limits memory for tracked double-spends.
	nsplitMaxPending = 50

	// nsplitMinHoldEpochs is the minimum number of epochs a partition must be
	// active before DoNetworkHeal will reconnect. Ensures forks have time to
	// form at EC finality depth (head-20).
	nsplitMinHoldEpochs = 10

	// EC security threshold — adversary above this can break EC (Wang 2023, m=5)
	ecThresholdPct = 20.0

	// F3 quorum threshold — need > 67% honest power for F3 to finalize
	f3QuorumPct = 67.0
)

// bisectionResult carries the power analysis from a partition for condition-specific assertions.
type bisectionResult struct {
	strategy      string  // "star", "bilateral", "isolation"
	adversaryPct  float64 // hub/isolated miner's power percentage
	honestPct     float64 // remaining honest power
	ecVulnerable  bool    // adversary >= 20%
	f3HasQuorum   bool    // honest > 67%
	expected      string  // human-readable expected outcome
}

// activePartition tracks the currently active network partition so that
// DoNetworkHeal can run post-partition verification when it reconnects.
type activePartition struct {
	result       *bisectionResult
	preF3Inst    uint64
	createdAt    time.Time
	startHeight  abi.ChainEpoch
}

var (
	currentPartition   *activePartition
	currentPartitionMu sync.Mutex
)

// pendingDoubleSpend tracks a pair of conflicting transactions for later verification.
// Carries the partition context at injection time so assertions match expected behavior.
type pendingDoubleSpend struct {
	fromAddr    address.Address
	nonce       uint64
	cidA        cid.Cid
	cidB        cid.Cid
	nodeA       string
	nodeB       string
	injectedAt  abi.ChainEpoch // chain head when injected
	attackType  string         // "double-spend", "fee-snipe", "balance-drain"
	f3HasQuorum bool           // whether F3 had honest quorum at injection time
	ecSafe      bool           // whether EC was safe (adversary < 20%) at injection time
}

var (
	pendingDoubleSpends   []pendingDoubleSpend
	pendingDoubleSpendsMu sync.Mutex
)

// ===========================================================================
// DoNetworkBisection — Create Partition Topologies (non-blocking)
// ===========================================================================

// DoNetworkBisection picks a random partition strategy and creates the topology.
// Returns immediately so the deck keeps spinning — transfers, gas wars, deploys,
// and adversarial vectors all run while the partition is active. Verification
// happens when DoNetworkHeal is drawn from the deck.
func DoNetworkBisection() {
	if len(nodeKeys) < 2 {
		return
	}

	// Wait for chain to mature past EC finality depth before partitioning.
	// Splitting before finality works means no forks can be detected.
	if !allNodesPastEpoch(f3MinEpoch) {
		return
	}

	// Don't stack partitions — if one is already active, skip
	currentPartitionMu.Lock()
	if currentPartition != nil {
		currentPartitionMu.Unlock()
		debugLog("[nsplit] partition already active, skipping")
		return
	}
	currentPartitionMu.Unlock()

	var result *bisectionResult
	strategy := rngIntn(100)
	switch {
	case strategy < 40:
		result = doStarSplit()
	case strategy < 80:
		result = doBilateralSplit()
	default:
		result = doFullIsolation()
	}

	if result == nil {
		return
	}

	// Confirm each strategy is reachable for Antithesis coverage
	assert.Reachable("N-split strategy executed", map[string]any{
		"strategy":     result.strategy,
		"adversary_pct": result.adversaryPct,
		"honest_pct":   result.honestPct,
		"ec_vulnerable": result.ecVulnerable,
		"f3_has_quorum": result.f3HasQuorum,
		"expected":      result.expected,
	})

	// Snapshot F3 instance and chain height before partition
	var preF3Inst uint64
	lotusNode, _ := pickLotusNode()
	if lotusNode != nil {
		preF3Inst, _ = getF3Instance(lotusNode)
	}

	var startHeight abi.ChainEpoch
	for _, name := range nodeKeys {
		head, err := nodes[name].ChainHead(ctx)
		if err == nil && head.Height() > startHeight {
			startHeight = head.Height()
		}
	}

	// Store partition state — deck keeps running, DoNetworkHeal will verify
	currentPartitionMu.Lock()
	currentPartition = &activePartition{
		result:      result,
		preF3Inst:   preF3Inst,
		createdAt:   time.Now(),
		startHeight: startHeight,
	}
	currentPartitionMu.Unlock()

	log.Printf("[nsplit] partition created (deck continues spinning)")
}

// doStarSplit picks a hub node (power-aware) and disconnects all non-hub
// nodes from each other. The hub stays connected to everyone.
// This simulates the n-split attack topology from Wang et al. 2023.
func doStarSplit() *bisectionResult {
	// Pick hub (adversary role) based on power table
	lotusNode, _ := pickLotusNode()
	if lotusNode == nil {
		return nil
	}

	table := getF3PowerTable(lotusNode)
	if len(table) < 2 {
		return nil
	}

	// Pick a random miner as hub
	hubMiner := table[rngIntn(len(table))]
	hubName := minerToNodeName(hubMiner.addr)
	if hubName == "" {
		return nil
	}

	// Get the hub's peer ID so we can identify it
	hubAddrInfo, err := nodes[hubName].NetAddrsListen(ctx)
	if err != nil {
		return nil
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

	expected := classifyExpected(hubMiner.pct, f3HasQuorum)
	log.Printf("[nsplit] EC threshold: vulnerable=%v (%.1f%% vs 20%%)", ecVulnerable, hubMiner.pct)
	log.Printf("[nsplit] F3 quorum: honest has quorum=%v (%.1f%% honest, need >67%%)", f3HasQuorum, totalHonest)
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

	return &bisectionResult{
		strategy:     "star",
		adversaryPct: hubMiner.pct,
		honestPct:    totalHonest,
		ecVulnerable: ecVulnerable,
		f3HasQuorum:  f3HasQuorum,
		expected:     expected,
	}
}

// doBilateralSplit disconnects two random nodes from each other.
// Creates partial mesh degradation.
func doBilateralSplit() *bisectionResult {
	nameA, nameB, nodeA, nodeB := pickTwoDistinctNodes()
	if nodeA == nil {
		return nil
	}

	// Get B's peer ID
	addrInfoB, err := nodeB.NetAddrsListen(ctx)
	if err != nil {
		return nil
	}

	// Disconnect A from B
	disconnected := false
	peers, err := nodeA.NetPeers(ctx)
	if err != nil {
		return nil
	}
	for _, p := range peers {
		if p.ID == addrInfoB.ID {
			if err := nodeA.NetDisconnect(ctx, p.ID); err == nil {
				disconnected = true
			}
			break
		}
	}

	if !disconnected {
		return nil
	}

	log.Printf("[nsplit] bilateral split: %s ↔ %s disconnected", nameA, nameB)

	// Bilateral splits create partial degradation — not a full adversary scenario
	return &bisectionResult{
		strategy:     "bilateral",
		adversaryPct: 0,
		honestPct:    100,
		ecVulnerable: false,
		f3HasQuorum:  true,
		expected:     "partial mesh degradation — forks unlikely but possible under load",
	}
}

// doFullIsolation disconnects one node from ALL peers.
// Simulates an adversary going private to build a competing chain.
func doFullIsolation() *bisectionResult {
	// Power-aware: pick based on power table when available
	victimName, victimPower := pickReorgVictim()
	victim := nodes[victimName]

	peers, err := victim.NetPeers(ctx)
	if err != nil || len(peers) == 0 {
		return nil
	}

	disconnected := 0
	for _, p := range peers {
		if err := victim.NetDisconnect(ctx, p.ID); err == nil {
			disconnected++
		}
	}

	honestPct := 100.0 - victimPower
	ecVulnerable := victimPower >= ecThresholdPct
	f3HasQuorum := honestPct > f3QuorumPct

	expected := classifyExpected(victimPower, f3HasQuorum)

	log.Printf("[nsplit] === bisection: full isolation ===")
	log.Printf("[nsplit] isolated: %s (%.1f%% power)", victimName, victimPower)
	log.Printf("[nsplit] honest power: %.1f%%", honestPct)
	log.Printf("[nsplit] EC threshold: vulnerable=%v (%.1f%% vs 20%%)", ecVulnerable, victimPower)
	log.Printf("[nsplit] F3 quorum: honest has quorum=%v (%.1f%% honest, need >67%%)", f3HasQuorum, honestPct)
	log.Printf("[nsplit] expected: %s", expected)
	log.Printf("[nsplit] disconnected %d peers", disconnected)

	return &bisectionResult{
		strategy:     "isolation",
		adversaryPct: victimPower,
		honestPct:    honestPct,
		ecVulnerable: ecVulnerable,
		f3HasQuorum:  f3HasQuorum,
		expected:     expected,
	}
}

// classifyExpected returns a human-readable expected outcome based on
// adversary power and whether honest nodes have F3 quorum.
func classifyExpected(adversaryPct float64, f3HasQuorum bool) string {
	switch {
	case adversaryPct < ecThresholdPct:
		return "EC safe, F3 safe — no attack should succeed"
	case f3HasQuorum:
		return "EC VULNERABLE, F3 safe — forks expected but F3 protects"
	case adversaryPct < 50:
		return "EC VULNERABLE, F3 VULNERABLE — both may fail"
	default:
		return "EC BROKEN, F3 BROKEN — catastrophic consensus failure expected"
	}
}

// ===========================================================================
// verifyBisectionOutcome — Condition-Specific Post-Heal Assertions
// ===========================================================================

// verifyBisectionOutcome checks F3 and EC behavior against the expected
// outcome for the partition's power conditions.
func verifyBisectionOutcome(result *bisectionResult, preF3Inst uint64) {
	if result == nil {
		return
	}

	// Check F3 progress
	var postF3Inst uint64
	var f3ok bool
	for _, name := range nodeKeys {
		if nodeType(name) != "lotus" {
			continue
		}
		inst, ok := getF3Instance(nodes[name])
		if ok && inst > postF3Inst {
			postF3Inst = inst
			f3ok = true
		}
	}

	f3Advanced := f3ok && postF3Inst > preF3Inst

	log.Printf("[nsplit] === post-heal verification ===")
	log.Printf("[nsplit] strategy: %s | adversary: %.1f%% | honest: %.1f%%",
		result.strategy, result.adversaryPct, result.honestPct)
	log.Printf("[nsplit] F3 instance: %d → %d (advanced=%v)", preF3Inst, postF3Inst, f3Advanced)
	log.Printf("[nsplit] expected: %s", result.expected)

	// Skip F3 assertions if F3 hasn't bootstrapped yet — instance 0 means
	// F3 never started, not that it stalled due to our partition.
	if preF3Inst == 0 && postF3Inst == 0 {
		log.Printf("[nsplit] SKIP: F3 not yet bootstrapped (instance=0), skipping F3 assertions")
	} else if result.f3HasQuorum {
		// F3 SHOULD advance — honest power > 67%
		assert.Always(f3Advanced, "F3 advances during partition when honest power > 67%", map[string]any{
			"strategy":      result.strategy,
			"adversary_pct": result.adversaryPct,
			"honest_pct":    result.honestPct,
			"f3_pre":        preF3Inst,
			"f3_post":       postF3Inst,
			"expected":      result.expected,
		})

		if f3Advanced {
			log.Printf("[nsplit] PASS: F3 protected (honest %.1f%% > 67%%, instance %d→%d)",
				result.honestPct, preF3Inst, postF3Inst)
		} else {
			log.Printf("[nsplit] FAIL: F3 did NOT advance despite honest %.1f%% > 67%% (instance stuck at %d)",
				result.honestPct, postF3Inst)
		}
	} else {
		// F3 MAY stall — honest power <= 67%
		assert.Sometimes(f3Advanced, "F3 may advance during partition when honest power <= 67%", map[string]any{
			"strategy":      result.strategy,
			"adversary_pct": result.adversaryPct,
			"honest_pct":    result.honestPct,
			"f3_pre":        preF3Inst,
			"f3_post":       postF3Inst,
			"expected":      result.expected,
		})

		if !f3Advanced {
			log.Printf("[nsplit] EXPECTED: F3 stalled (honest %.1f%% <= 67%%, instance stuck at %d)",
				result.honestPct, postF3Inst)
		} else {
			log.Printf("[nsplit] SURPRISING: F3 advanced despite honest %.1f%% <= 67%% (instance %d→%d)",
				result.honestPct, preF3Inst, postF3Inst)
		}
	}

	if result.ecVulnerable {
		log.Printf("[nsplit] EC: VULNERABLE (adversary %.1f%% >= 20%%) — forks expected at this power level",
			result.adversaryPct)
	} else {
		log.Printf("[nsplit] EC: safe (adversary %.1f%% < 20%%)", result.adversaryPct)
	}
}

// ===========================================================================
// DoNetworkHeal — Reconnect All Nodes + Verify Partition Outcome
// ===========================================================================

// DoNetworkHeal reconnects all nodes to a full mesh. Only runs if a
// partition is active and has been held for at least nsplitMinHoldEpochs.
// After reconnecting, runs post-partition F3/EC verification against the
// expected outcome recorded when the partition was created.
func DoNetworkHeal() {
	if len(nodeKeys) < 2 {
		return
	}

	// Check if a partition is active
	currentPartitionMu.Lock()
	partition := currentPartition
	if partition == nil {
		currentPartitionMu.Unlock()
		return // no partition to heal
	}

	// Enforce minimum hold — forks need time to form at EC finality depth.
	// Use max height across all nodes to avoid jitter from partitioned nodes.
	var currentHeight abi.ChainEpoch
	for _, name := range nodeKeys {
		head, err := nodes[name].ChainHead(ctx)
		if err == nil && head.Height() > currentHeight {
			currentHeight = head.Height()
		}
	}
	epochsHeld := currentHeight - partition.startHeight
	if epochsHeld < 0 {
		epochsHeld = 0 // reorg dropped max height below partition start
	}
	if epochsHeld < nsplitMinHoldEpochs {
		currentPartitionMu.Unlock()
		debugLog("[nsplit] heal skipped: partition held %d/%d epochs", epochsHeld, nsplitMinHoldEpochs)
		return
	}

	// Clear partition state before healing
	currentPartition = nil
	currentPartitionMu.Unlock()

	// Reconnect all nodes
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

	log.Printf("[nsplit] healed network: %d connections established (partition held %d epochs, %s)",
		reconnected, epochsHeld, time.Since(partition.createdAt).Round(time.Second))

	assert.Sometimes(reconnected > 0, "Network heal reconnected nodes", map[string]any{
		"connections": reconnected,
		"nodes":       len(nodeKeys),
		"epochs_held": epochsHeld,
	})

	// Verify partition outcome against expected behavior
	verifyBisectionOutcome(partition.result, partition.preF3Inst)
}

// ===========================================================================
// DoAdversarialDuringFork — Opportunistic Attacks During Active Forks
// ===========================================================================

// DoAdversarialDuringFork checks if a fork currently exists at either the
// head level or finalized level. Head-level forks are the real attack window —
// an exchange credits a deposit when it sees a tx at the chain head, not at
// finalized depth. Without F3, head-level forks are common and exploitable.
//
// Attack strategies:
//   - Double-spend: same nonce, different recipients, different forks
//   - Fee-snipe: low-fee tx on fork A, high-fee replacement on fork B
//   - Balance drain: send full balance to different recipients on different forks
//
// All attacks are tracked for post-convergence verification by DoAdversarialVerify.
func DoAdversarialDuringFork() {
	if len(nodeKeys) < 2 {
		return
	}

	nameA, nameB, nodeA, nodeB := pickTwoDistinctNodes()
	if nodeA == nil {
		return
	}

	headA, errA := nodeA.ChainHead(ctx)
	headB, errB := nodeB.ChainHead(ctx)
	if errA != nil || errB != nil {
		return
	}

	// Skip if chain is too young
	if headA.Height() < f3MinEpoch || headB.Height() < f3MinEpoch {
		return
	}

	// Skip if nodes are at wildly different heights — that's sync lag, not a fork
	heightDiff := headA.Height() - headB.Height()
	if heightDiff < 0 {
		heightDiff = -heightDiff
	}
	if heightDiff > 10 {
		return
	}

	// Check if a partition is active and what it expects
	currentPartitionMu.Lock()
	partitionActive := currentPartition != nil
	var partitionECVulnerable bool
	if partitionActive && currentPartition.result != nil {
		partitionECVulnerable = currentPartition.result.ecVulnerable
	}
	currentPartitionMu.Unlock()

	// Check for fork: nodes at similar height but different tipset keys
	headFork := headA.Key() != headB.Key()

	if !headFork {
		if partitionActive && partitionECVulnerable {
			assert.Sometimes(false, "EC fork detected during vulnerable partition", map[string]any{
				"node_a":        nameA,
				"node_b":        nameB,
				"height_a":      headA.Height(),
				"height_b":      headB.Height(),
				"level":         "head",
				"partition":     true,
				"ec_vulnerable": true,
			})
		}
		return
	}

	// Fork detected at head level
	if partitionActive && partitionECVulnerable {
		assert.Sometimes(true, "EC fork detected during vulnerable partition", map[string]any{
			"node_a":        nameA,
			"node_b":        nameB,
			"height_a":      headA.Height(),
			"height_b":      headB.Height(),
			"level":         "head",
			"partition":     true,
			"ec_vulnerable": true,
		})
	}

	// Pick attack strategy
	attack := rngIntn(3)
	attackNames := []string{"double-spend", "fee-snipe", "balance-drain"}
	log.Printf("[nsplit] fork detected at HEAD (%s h=%d vs %s h=%d) — attack: %s",
		nameA, headA.Height(), nameB, headB.Height(), attackNames[attack])

	switch attack {
	case 0:
		doForkDoubleSpend(nameA, nameB, nodeA, nodeB)
	case 1:
		doForkFeeSniping(nameA, nameB, nodeA, nodeB)
	case 2:
		doForkBalanceDrain(nameA, nameB, nodeA, nodeB, headA)
	}
}

// doForkDoubleSpend sends conflicting transactions (same nonce, different
// recipients) to nodes on different fork branches.
func doForkDoubleSpend(nameA, nameB string, nodeA, nodeB api.FullNode) {
	fromAddr, fromKI := pickWallet()
	toAddrA, _ := pickWallet()
	toAddrB, _ := pickWallet()
	if fromAddr == toAddrA || fromAddr == toAddrB || toAddrA == toAddrB {
		return
	}

	currentNonce := nonces[fromAddr]

	msgA := baseMsg(fromAddr, toAddrA, abi.NewTokenAmount(1))
	cidA, okA := pushMsgManualNonce(nodeA, msgA, fromKI, currentNonce, "nsplit-dspend-A")

	msgB := baseMsg(fromAddr, toAddrB, abi.NewTokenAmount(1))
	cidB, okB := pushMsgManualNonce(nodeB, msgB, fromKI, currentNonce, "nsplit-dspend-B")

	nonces[fromAddr]++

	if !okA || !okB {
		return
	}

	trackPendingAttack(fromAddr, currentNonce, cidA, cidB, nameA, nameB, nodeA, "double-spend")
}

// doForkFeeSniping sends a low-fee tx to one fork and a high-fee replacement
// (same nonce) to the other. Tests fee economics across reorgs.
func doForkFeeSniping(nameA, nameB string, nodeA, nodeB api.FullNode) {
	fromAddr, fromKI := pickWallet()
	toAddr, _ := pickWallet()
	if fromAddr == toAddr {
		return
	}

	currentNonce := nonces[fromAddr]

	// Low-fee to fork A
	msgLow := baseMsg(fromAddr, toAddr, abi.NewTokenAmount(1))
	msgLow.GasPremium = abi.NewTokenAmount(100)
	msgLow.GasFeeCap = abi.NewTokenAmount(100_000)
	cidLow, okLow := pushMsgManualNonce(nodeA, msgLow, fromKI, currentNonce, "nsplit-feesnipe-low")

	// High-fee to fork B (same nonce, same recipient)
	msgHigh := baseMsg(fromAddr, toAddr, abi.NewTokenAmount(1))
	msgHigh.GasPremium = abi.NewTokenAmount(50_000)
	msgHigh.GasFeeCap = abi.NewTokenAmount(200_000)
	cidHigh, okHigh := pushMsgManualNonce(nodeB, msgHigh, fromKI, currentNonce, "nsplit-feesnipe-high")

	nonces[fromAddr]++

	if !okLow || !okHigh {
		return
	}

	trackPendingAttack(fromAddr, currentNonce, cidLow, cidHigh, nameA, nameB, nodeA, "fee-snipe")
}

// doForkBalanceDrain sends the full wallet balance to different recipients
// on different fork branches. Tests balance accounting across deep reorgs.
func doForkBalanceDrain(nameA, nameB string, nodeA, nodeB api.FullNode, finA *types.TipSet) {
	fromAddr, fromKI := pickWallet()
	toAddrA, _ := pickWallet()
	toAddrB, _ := pickWallet()
	if fromAddr == toAddrA || fromAddr == toAddrB || toAddrA == toAddrB {
		return
	}

	// Query actual balance
	actor, err := nodeA.StateGetActor(ctx, fromAddr, finA.Key())
	if err != nil || actor == nil {
		return
	}

	// Reserve 1 FIL for gas
	gasBudget := abi.NewTokenAmount(1_000_000_000_000_000_000)
	if actor.Balance.LessThanEqual(gasBudget) {
		return
	}
	drainAmount := abi.TokenAmount{Int: new(big.Int).Sub(actor.Balance.Int, gasBudget.Int)}

	currentNonce := nonces[fromAddr]

	msgA := baseMsg(fromAddr, toAddrA, drainAmount)
	cidA, okA := pushMsgManualNonce(nodeA, msgA, fromKI, currentNonce, "nsplit-drain-A")

	msgB := baseMsg(fromAddr, toAddrB, drainAmount)
	cidB, okB := pushMsgManualNonce(nodeB, msgB, fromKI, currentNonce, "nsplit-drain-B")

	nonces[fromAddr]++

	if !okA || !okB {
		return
	}

	trackPendingAttack(fromAddr, currentNonce, cidA, cidB, nameA, nameB, nodeA, "balance-drain")
}

// trackPendingAttack records a conflicting tx pair for later verification.
// Captures the current partition's power conditions so assertions can be
// conditional: F3 with quorum should always prevent double-spends, but
// when both EC and F3 are broken a successful double-spend is expected.
func trackPendingAttack(fromAddr address.Address, nonce uint64, cidA, cidB cid.Cid, nameA, nameB string, refNode api.FullNode, attackType string) {
	head, err := refNode.ChainHead(ctx)
	if err != nil {
		return
	}

	// Snapshot partition context at injection time
	var f3HasQuorum, ecSafe bool
	currentPartitionMu.Lock()
	if currentPartition != nil && currentPartition.result != nil {
		f3HasQuorum = currentPartition.result.f3HasQuorum
		ecSafe = !currentPartition.result.ecVulnerable
	}
	currentPartitionMu.Unlock()

	pendingDoubleSpendsMu.Lock()
	if len(pendingDoubleSpends) >= nsplitMaxPending {
		pendingDoubleSpends = pendingDoubleSpends[1:]
	}
	pendingDoubleSpends = append(pendingDoubleSpends, pendingDoubleSpend{
		fromAddr:    fromAddr,
		nonce:       nonce,
		cidA:        cidA,
		cidB:        cidB,
		nodeA:       nameA,
		nodeB:       nameB,
		injectedAt:  head.Height(),
		attackType:  attackType,
		f3HasQuorum: f3HasQuorum,
		ecSafe:      ecSafe,
	})
	pendingDoubleSpendsMu.Unlock()

	log.Printf("[nsplit] %s injected: from=%s nonce=%d cidA=%s→%s cidB=%s→%s (f3_quorum=%v ec_safe=%v)",
		attackType, fromAddr, nonce, cidStr(cidA), nameA, cidStr(cidB), nameB, f3HasQuorum, ecSafe)

	assert.Sometimes(true, "Adversarial attack injected during fork", map[string]any{
		"attack":      attackType,
		"from":        fromAddr.String(),
		"nonce":       nonce,
		"node_a":      nameA,
		"node_b":      nameB,
		"f3_quorum":   f3HasQuorum,
		"ec_safe":     ecSafe,
	})
}

// ===========================================================================
// DoAdversarialVerify — Post-Convergence Safety Check
// ===========================================================================

// DoAdversarialVerify checks pending attack records. For records where
// enough epochs have passed since injection, it verifies that at most one
// of the conflicting transactions was included on-chain.
func DoAdversarialVerify() {
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

		details := map[string]any{
			"from":          ds.fromAddr.String(),
			"nonce":         ds.nonce,
			"attack":        ds.attackType,
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
			"f3_quorum":     ds.f3HasQuorum,
			"ec_safe":       ds.ecSafe,
		}

		// Assertion strength depends on what should have protected us:
		//
		// F3 quorum    EC safe    Expected
		// ─────────    ───────    ────────
		// true         *          Always safe — F3 prevents conflicting finalization
		// false        true       Always safe — EC alone protects below 20% adversary
		// false        false      Sometimes safe — both vulnerable, double-spend may succeed
		if ds.f3HasQuorum {
			assert.Always(safe, "Double-spend safety: F3 quorum should prevent both txs landing", details)
			if !safe {
				log.Printf("[nsplit] SAFETY VIOLATION (F3 HAD QUORUM): %s from=%s nonce=%d — both txs landed",
					ds.attackType, ds.fromAddr, ds.nonce)
			}
		} else if ds.ecSafe {
			assert.Always(safe, "Double-spend safety: EC safe (adversary <20%%) should prevent both txs landing", details)
			if !safe {
				log.Printf("[nsplit] SAFETY VIOLATION (EC SAFE): %s from=%s nonce=%d — both txs landed",
					ds.attackType, ds.fromAddr, ds.nonce)
			}
		} else {
			// Both EC and F3 vulnerable — double-spend succeeding is possible
			assert.Sometimes(safe, "Double-spend safety: both EC and F3 vulnerable, attack may succeed", details)
			if !safe {
				log.Printf("[nsplit] EXPECTED: %s succeeded (EC vulnerable, F3 no quorum): from=%s nonce=%d",
					ds.attackType, ds.fromAddr, ds.nonce)
			}
		}

		if safe {
			debugLog("[nsplit] %s verified safe: from=%s nonce=%d landed=%d (after %d epochs, f3_quorum=%v ec_safe=%v)",
				ds.attackType, ds.fromAddr, ds.nonce, landed, epochsSince, ds.f3HasQuorum, ds.ecSafe)
		}

		assert.Sometimes(landed >= 1, "Adversarial attack: at least one tx eventually included", map[string]any{
			"attack":       ds.attackType,
			"from":         ds.fromAddr.String(),
			"nonce":        ds.nonce,
			"total_landed": landed,
		})

		// Verified — don't keep
	}

	pendingDoubleSpends = remaining
}
