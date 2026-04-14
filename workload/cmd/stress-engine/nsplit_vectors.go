package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"sync/atomic"
	"time"

	"github.com/antithesishq/antithesis-sdk-go/assert"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p/core/peer"
)

// ===========================================================================
// Consensus Integration Test — Lifecycle-Based EC/F3 Safety Proof
//
// Structured, reproducible test cycles that prove:
//   - Without F3: double-spend is possible at 20%+ adversary power
//   - With F3: double-spend is prevented when F3 has quorum
//
// Runs as a background goroutine. Each cycle:
//   1. Create partition (full isolation of largest miner)
//   2. Wait for chains to diverge past EC finality depth
//   3. Inject adversarial action (rotates: double-spend, gas-premium-frontrun, balance-exhaustion)
//   4. Heal partition, wait for convergence
//   5. Verify outcome with condition-specific assertions
//   6. Log structured summary
//
// The deck continues running background activity (transfers, gas wars)
// during the partition, making the test more realistic.
// ===========================================================================

const (
	ecThresholdPct    = 20.0            // EC vulnerability threshold (Wang 2023, m=5)
	f3QuorumPct       = 67.0            // F3 honest power requirement
	convergenceBuffer = 25              // epochs past heal before verification
	divergeMinEpochs  = 15              // min epoch diff before injecting attack (was 5 — too shallow for txs to land)
	divergeTimeout    = 5 * time.Minute
	settlementTimeout = 10 * time.Minute
	testCooldown      = 30 * time.Second
	attackMineTimeout = 60 * time.Second // max wait for attack txs to be mined before healing
)

// partitionActive signals to deck vectors that a test partition is active.
var partitionActive atomic.Bool

// splitStrategy enumerates the partition topologies.
type splitStrategy int

const (
	splitFullIsolation splitStrategy = iota // one miner disconnected from all
	splitStar                              // hub miner connected to all, honest miners isolated from each other
	splitBisection                         // network split into two ~equal halves
	splitCount                             // sentinel for rotation
)

func (s splitStrategy) String() string {
	switch s {
	case splitFullIsolation:
		return "full-isolation"
	case splitStar:
		return "star-split"
	case splitBisection:
		return "50/50-bisection"
	default:
		return "unknown"
	}
}

// attackType enumerates the adversarial strategies.
type attackType int

const (
	attackDoubleSpend        attackType = iota // same nonce, different recipients
	attackGasPremiumFrontrun                   // same nonce, different gas premiums
	attackBalanceExhaustion                    // same nonce, full balance to different recipients
	attackCount                                // sentinel for rotation
)

func (a attackType) String() string {
	switch a {
	case attackDoubleSpend:
		return "double-spend"
	case attackGasPremiumFrontrun:
		return "gas-premium-frontrun"
	case attackBalanceExhaustion:
		return "balance-exhaustion"
	default:
		return "unknown"
	}
}

// blockedPeer records a peer that was blocked on a specific node,
// so healPartition can remove the exact blocklist entries it added.
type blockedPeer struct {
	onNode string  // node name where the block was added
	peerID peer.ID // blocked peer ID
}

// splitResult carries the partition state for heal + verification.
type splitResult struct {
	strategy      splitStrategy
	adversaryName string
	adversaryPct  float64
	honestPct     float64
	honestNode    string           // a node on the honest side (for injection)
	advNode       api.FullNode     // adversary's API handle
	savedPeers    []peer.AddrInfo  // for healing (reconnect)
	blocked       []blockedPeer    // for healing (unblock)
	ecVulnerable  bool
	f3HasQuorum   bool
	expected      string
}

// attackResult captures the injected attack for verification.
type attackResult struct {
	attack     attackType
	fromAddr   address.Address
	nonce      uint64
	cidA       cid.Cid         // sent to honest node
	cidB       cid.Cid         // sent to adversary node
	honestNode string
	advNode    string
	amount     abi.TokenAmount // transfer amount for balance verification
	preBalance abi.TokenAmount // sender balance snapshot before attack
}

// ---------------------------------------------------------------------------
// F3 Detection
// ---------------------------------------------------------------------------

func isF3Active() bool {
	for _, name := range nodeKeys {
		if nodeType(name) != "lotus" {
			continue
		}
		_, ok := getF3Instance(nodes[name])
		if ok {
			return true
		}
	}
	return false
}

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

// ---------------------------------------------------------------------------
// Lifecycle Entry Point
// ---------------------------------------------------------------------------

func startConsensusTestLifecycle() {
	if envOrDefault("STRESS_CONSENSUS_TEST", "0") != "1" {
		log.Println("[consensus-test] disabled (STRESS_CONSENSUS_TEST != 1)")
		return
	}

	go func() {
		log.Println("[consensus-test] lifecycle started — waiting for chain maturity")

		for {
			if allNodesPastEpoch(f3MinEpoch) {
				break
			}
			time.Sleep(10 * time.Second)
		}

		cycleNum := 0
		for {
			select {
			case <-ctx.Done():
				return
			default:
				cycleNum++
				runConsensusCycle(cycleNum)
				time.Sleep(testCooldown)
			}
		}
	}()
}

// ---------------------------------------------------------------------------
// Test Cycle
// ---------------------------------------------------------------------------

func runConsensusCycle(cycleNum int) {
	if len(nodeKeys) < 2 {
		return
	}

	f3Active := isF3Active()
	lotusNode, _ := pickLotusNode()
	if lotusNode == nil {
		return
	}
	table := getF3PowerTable(lotusNode)
	if len(table) < 2 {
		return
	}

	// Rotate strategy and attack across cycles (3 splits × 3 attacks = 9 combinations)
	split := splitStrategy(cycleNum % int(splitCount))
	attack := attackType((cycleNum / int(splitCount)) % int(attackCount))

	// --- Header ---
	log.Printf("[consensus-test] === CYCLE %d === strategy=%s attack=%s f3=%v", cycleNum, split, attack, f3Active)
	for _, m := range table {
		log.Printf("[consensus-test]   %s: %.1f%% power", minerToNodeName(m.addr), m.pct)
	}

	// --- Snapshot ---
	var preF3Inst uint64
	if f3Active {
		preF3Inst, _ = getF3Instance(lotusNode)
	}
	preHead, err := lotusNode.ChainHead(ctx)
	if err != nil {
		return
	}

	// --- Create partition based on strategy ---
	sr := createPartition(split, table, f3Active)
	if sr == nil {
		log.Printf("[consensus-test] partition creation failed, skipping cycle")
		return
	}

	partitionActive.Store(true)
	log.Printf("[consensus-test] PARTITION: %s", split)
	log.Printf("[consensus-test]   adversary: %s (%.1f%%)", sr.adversaryName, sr.adversaryPct)
	log.Printf("[consensus-test]   honest: %.1f%% | EC vulnerable: %v | F3 quorum: %v",
		sr.honestPct, sr.ecVulnerable, sr.f3HasQuorum)
	log.Printf("[consensus-test]   expected: %s", sr.expected)

	// --- Divergence ---
	waitForDivergence(sr.honestNode, sr.adversaryName, sr.advNode)

	// --- Inject attack ---
	ar := injectAttack(attack, sr.honestNode, sr.adversaryName, sr.advNode)
	if ar == nil {
		log.Printf("[consensus-test] attack injection failed, healing")
		partitionActive.Store(false)
		healPartition(sr)
		return
	}

	// --- Wait for attack txs to be mined on their respective forks ---
	// Without this, healing immediately after injection means the honest majority
	// reconverges before the adversary's tx is included in a block, so the tx
	// is pruned from the mempool and never lands on the final chain.
	mined := waitForAttackMined(ar, sr)
	log.Printf("[consensus-test] attack mining: honest=%v adversary=%v", mined.honestMined, mined.advMined)

	// --- Heal ---
	log.Printf("[consensus-test] HEALING partition...")
	partitionActive.Store(false)
	healPartition(sr)

	// Check adversary is still alive after heal — Antithesis may have killed it
	_, advAliveErr := sr.advNode.ChainHead(ctx)
	if advAliveErr != nil {
		log.Printf("[consensus-test] adversary %s unreachable after heal: %v — skipping hard assertions", sr.adversaryName, advAliveErr)
		assert.Sometimes(true, "Consensus cycle ran but adversary was killed by Antithesis", map[string]any{
			"cycle": cycleNum, "adversary": sr.adversaryName,
		})
		return
	}

	converged := waitForConvergence(sr.adversaryName)
	log.Printf("[consensus-test] convergence: %v", converged)

	// --- Settlement ---
	waitForSettlement(lotusNode, preHead.Height())

	// --- Re-check F3 (may have stalled during partition — don't assume pre-partition state) ---
	postF3Active := isF3Active()
	if f3Active && !postF3Active {
		log.Printf("[consensus-test] WARNING: F3 was active pre-partition but stalled during cycle")
		// Downgrade: if F3 stalled, don't assert F3 safety guarantees
		sr.f3HasQuorum = false
		sr.expected = classifyExpected(sr.adversaryPct, false)
	}

	// --- Verify on honest-side node (not random — avoids adversary's divergent view) ---
	verifyNode := nodes[sr.honestNode]
	landed := verifyOutcome(verifyNode, ar, cycleNum, sr, postF3Active, attack)
	verifyEconomicImpact(verifyNode, ar, sr, landed)

	// --- F3 health ---
	if f3Active {
		postF3, _ := getF3Instance(verifyNode)
		log.Printf("[consensus-test] F3 post-heal: %d→%d (active=%v)", preF3Inst, postF3, postF3Active)
	}

	// --- Structured summary ---
	verdict := "UNKNOWN"
	switch {
	case sr.f3HasQuorum && landed <= 1:
		verdict = "PASS — F3 protected"
	case sr.f3HasQuorum && landed > 1:
		verdict = "FAIL — double-spend despite F3"
	case !sr.f3HasQuorum && !sr.ecVulnerable && landed <= 1:
		verdict = "PASS — EC protected"
	case sr.ecVulnerable && landed == 2:
		verdict = "CONFIRMED — EC vulnerability exploited"
	case sr.ecVulnerable && landed == 1:
		verdict = "EC resolved — honest majority won"
	case landed == 0:
		verdict = "INCONCLUSIVE — neither tx landed"
	}

	summaryJSON, _ := json.Marshal(map[string]any{
		"event":         "consensus_cycle_result",
		"cycle":         cycleNum,
		"strategy":      split.String(),
		"adversary":     sr.adversaryName,
		"adversary_pct": sr.adversaryPct,
		"attack":        attack.String(),
		"f3_active":     f3Active,
		"f3_has_quorum": sr.f3HasQuorum,
		"ec_vulnerable": sr.ecVulnerable,
		"landed":        landed,
		"verdict":       verdict,
	})
	log.Printf("[consensus-test] RESULT %s", string(summaryJSON))
}

// ---------------------------------------------------------------------------
// Partition Creation
// ---------------------------------------------------------------------------

// createPartition executes the given split strategy and returns the partition
// state needed for heal + verification. Returns nil if the split fails.
func createPartition(split splitStrategy, table []minerPowerInfo, f3Active bool) *splitResult {
	switch split {
	case splitFullIsolation:
		return createFullIsolation(table, f3Active)
	case splitStar:
		return createStarSplit(table, f3Active)
	case splitBisection:
		return createBisection(table, f3Active)
	default:
		return nil
	}
}

// fullIsolationIdx rotates which miner gets isolated across cycles.
// Cycle 0 → table[0] (40%), cycle 3 → table[1] (30%), cycle 6 → table[2] (20%), etc.
// This ensures we test both "F3 has quorum" (small adversary) and
// "F3 vulnerable" (large adversary) scenarios.
var fullIsolationIdx int

// createFullIsolation disconnects one miner from all peers.
// Topology: adversary alone vs honest majority together.
// Rotates the target across cycles so we cover all power postures:
//   - Isolate 40%: honest=60%, F3 quorum=false → EC+F3 both vulnerable
//   - Isolate 30%: honest=70%, F3 quorum=true  → F3 should protect
//   - Isolate 20%: honest=80%, F3 quorum=true  → F3 should protect
//   - Isolate 10%: honest=90%, F3 quorum=true  → F3 should protect
func createFullIsolation(table []minerPowerInfo, f3Active bool) *splitResult {
	idx := fullIsolationIdx % len(table)
	fullIsolationIdx++
	adversary := table[idx]
	advName := minerToNodeName(adversary.addr)
	if advName == "" {
		return nil
	}

	advNode := nodes[advName]
	peers, err := advNode.NetPeers(ctx)
	if err != nil || len(peers) == 0 {
		return nil
	}

	savedPeers := make([]peer.AddrInfo, len(peers))
	copy(savedPeers, peers)

	// Disconnect and block all peers on the adversary node.
	// Also block the adversary on each honest node (both directions).
	var blocked []blockedPeer
	disconnected := 0
	for _, p := range peers {
		if err := advNode.NetDisconnect(ctx, p.ID); err == nil {
			disconnected++
		}
	}

	// Block all peers on adversary
	blockPeerIDs := make([]peer.ID, 0, len(peers))
	for _, p := range peers {
		blockPeerIDs = append(blockPeerIDs, p.ID)
	}
	if err := advNode.NetBlockAdd(ctx, api.NetBlockList{Peers: blockPeerIDs}); err != nil {
		log.Printf("[consensus-test] NetBlockAdd on %s failed: %v", advName, err)
	} else {
		for _, pid := range blockPeerIDs {
			blocked = append(blocked, blockedPeer{onNode: advName, peerID: pid})
		}
	}

	// Block adversary on each honest node (including forest — prevents gossip bridge)
	advAddrInfo, _ := advNode.NetAddrsListen(ctx)
	for _, name := range nodeKeys {
		if name == advName {
			continue
		}
		if err := nodes[name].NetBlockAdd(ctx, api.NetBlockList{Peers: []peer.ID{advAddrInfo.ID}}); err != nil {
			log.Printf("[consensus-test] NetBlockAdd(%s) on %s failed: %v", advName, name, err)
		} else {
			blocked = append(blocked, blockedPeer{onNode: name, peerID: advAddrInfo.ID})
		}
		// Also disconnect existing connection
		nodes[name].NetDisconnect(ctx, advAddrInfo.ID)
	}

	// Block forest on adversary too (bidirectional — forest can't relay)
	for _, name := range nodeKeys {
		if nodeType(name) != "forest" {
			continue
		}
		forestAddrInfo, err := nodes[name].NetAddrsListen(ctx)
		if err != nil {
			continue
		}
		if err := advNode.NetBlockAdd(ctx, api.NetBlockList{Peers: []peer.ID{forestAddrInfo.ID}}); err != nil {
			log.Printf("[consensus-test] NetBlockAdd(forest) on %s failed: %v", advName, err)
		} else {
			blocked = append(blocked, blockedPeer{onNode: advName, peerID: forestAddrInfo.ID})
		}
		advNode.NetDisconnect(ctx, forestAddrInfo.ID)
	}

	honestPct := 100.0 - adversary.pct
	log.Printf("[consensus-test] full-isolation: disconnected+blocked %s (%.1f%%) from %d peers",
		advName, adversary.pct, disconnected)

	return &splitResult{
		strategy:      splitFullIsolation,
		adversaryName: advName,
		adversaryPct:  adversary.pct,
		honestPct:     honestPct,
		honestNode:    pickHonestNode(advName),
		advNode:       advNode,
		savedPeers:    savedPeers,
		blocked:       blocked,
		ecVulnerable:  adversary.pct >= ecThresholdPct,
		f3HasQuorum:   f3Active && honestPct > f3QuorumPct,
		expected:      classifyExpected(adversary.pct, f3Active && honestPct > f3QuorumPct),
	}
}

// createStarSplit implements the n-split attack from Wang et al. 2023 §6.
// Topology: adversary (hub) stays connected to ALL honest miners, but
// honest miners are isolated FROM EACH OTHER. Each honest miner can only
// communicate with the adversary.
//
// This is stronger than full fragmentation because the adversary:
//   - Receives blocks from all honest miners (information advantage)
//   - Can selectively relay or withhold blocks
//   - Honest miners see only their own blocks + what the adversary sends
//
// With 4:3:2:1 power:
//   - Adversary (40%) sees everyone's blocks
//   - lotus1 (30%) sees only adversary blocks
//   - lotus2 (20%) sees only adversary blocks
//   - lotus3 (10%) sees only adversary blocks
//   - F3 cannot reach quorum: no honest miner can talk to another
func createStarSplit(table []minerPowerInfo, f3Active bool) *splitResult {
	adversary := table[0] // largest miner
	advName := minerToNodeName(adversary.addr)
	if advName == "" {
		return nil
	}

	// Gather honest node names and peer IDs
	honestNames := []string{}
	peerIDs := map[string]peer.ID{} // nodeName -> peerID
	for _, m := range table {
		name := minerToNodeName(m.addr)
		if name == "" {
			continue
		}
		addrInfo, err := nodes[name].NetAddrsListen(ctx)
		if err == nil {
			peerIDs[name] = addrInfo.ID
		}
		if name != advName {
			honestNames = append(honestNames, name)
		}
	}

	// Isolate honest miners from each other (but NOT from adversary).
	// For each honest node: block all OTHER honest nodes, keep adversary connected.
	var allSavedPeers []peer.AddrInfo
	var blocked []blockedPeer
	totalDisconnected := 0

	for _, name := range honestNames {
		// Build block list: all other HONEST nodes (not adversary)
		var toBlock []peer.ID
		for _, otherHonest := range honestNames {
			if otherHonest == name {
				continue
			}
			if pid, ok := peerIDs[otherHonest]; ok {
				toBlock = append(toBlock, pid)
			}
		}

		// Also block forest0 if present (prevents gossip bridge — see audit issue #3)
		for _, n := range nodeKeys {
			if nodeType(n) == "forest" {
				if fAddrInfo, err := nodes[n].NetAddrsListen(ctx); err == nil {
					toBlock = append(toBlock, fAddrInfo.ID)
					blocked = append(blocked, blockedPeer{onNode: name, peerID: fAddrInfo.ID})
				}
			}
		}

		// Disconnect honest→honest peers (but keep honest→adversary)
		advPID := peerIDs[advName]
		peers, err := nodes[name].NetPeers(ctx)
		if err != nil {
			continue
		}
		for _, p := range peers {
			if p.ID == advPID {
				continue // keep adversary connection
			}
			if err := nodes[name].NetDisconnect(ctx, p.ID); err == nil {
				totalDisconnected++
				allSavedPeers = append(allSavedPeers, p)
			}
		}

		// Block other honest nodes
		if len(toBlock) > 0 {
			if err := nodes[name].NetBlockAdd(ctx, api.NetBlockList{Peers: toBlock}); err != nil {
				log.Printf("[consensus-test] NetBlockAdd on %s failed: %v", name, err)
			} else {
				for _, pid := range toBlock {
					blocked = append(blocked, blockedPeer{onNode: name, peerID: pid})
				}
			}
		}
	}

	// Also block forest on each honest node's REVERSE direction:
	// forest blocks all honest nodes (only keeps adversary if connected)
	for _, n := range nodeKeys {
		if nodeType(n) != "forest" {
			continue
		}
		var forestBlock []peer.ID
		for _, honestName := range honestNames {
			if pid, ok := peerIDs[honestName]; ok {
				forestBlock = append(forestBlock, pid)
			}
		}
		if len(forestBlock) > 0 {
			if err := nodes[n].NetBlockAdd(ctx, api.NetBlockList{Peers: forestBlock}); err != nil {
				log.Printf("[consensus-test] NetBlockAdd on %s (forest) failed: %v", n, err)
			} else {
				for _, pid := range forestBlock {
					blocked = append(blocked, blockedPeer{onNode: n, peerID: pid})
				}
			}
		}
	}

	honestPct := 100.0 - adversary.pct
	log.Printf("[consensus-test] n-split (star): adversary=%s (%.1f%%) as hub, %d honest miners isolated from each other",
		advName, adversary.pct, len(honestNames))
	for _, name := range honestNames {
		pct := 0.0
		for _, m := range table {
			if minerToNodeName(m.addr) == name {
				pct = m.pct
				break
			}
		}
		log.Printf("[consensus-test]   spoke: %s (%.1f%%) ←→ %s only", name, pct, advName)
	}

	// Pick first honest node for attack injection
	honestTarget := ""
	if len(honestNames) > 0 {
		honestTarget = honestNames[0]
	}

	return &splitResult{
		strategy:      splitStar,
		adversaryName: advName,
		adversaryPct:  adversary.pct,
		honestPct:     honestPct,
		honestNode:    honestTarget,
		advNode:       nodes[advName],
		savedPeers:    allSavedPeers,
		blocked:       blocked,
		ecVulnerable:  adversary.pct >= ecThresholdPct,
		f3HasQuorum:   false, // honest miners can't communicate — F3 cannot reach quorum
		expected:      classifyExpected(adversary.pct, false),
	}
}

// createBisection splits the network into two roughly equal halves.
// With 4:3:2:1 power: groupA = lotus0(40%)+lotus3(10%) = 50%,
//                      groupB = lotus1(30%)+lotus2(20%) = 50%.
// Neither side has majority. Tests fork resolution under maximum ambiguity.
func createBisection(table []minerPowerInfo, f3Active bool) *splitResult {
	if len(table) < 4 {
		// Need at least 4 miners for a meaningful bisection
		return createFullIsolation(table, f3Active) // fallback
	}

	// Split: biggest + smallest vs middle two
	// With sorted desc [40, 30, 20, 10]: groupA = [0]+[3] = 50%, groupB = [1]+[2] = 50%
	groupA := []minerPowerInfo{table[0], table[len(table)-1]}
	groupB := []minerPowerInfo{}
	for i := 1; i < len(table)-1; i++ {
		groupB = append(groupB, table[i])
	}

	groupAPower := 0.0
	groupANames := []string{}
	for _, m := range groupA {
		name := minerToNodeName(m.addr)
		if name == "" {
			continue
		}
		groupANames = append(groupANames, name)
		groupAPower += m.pct
	}

	groupBNames := []string{}
	groupBPower := 0.0
	for _, m := range groupB {
		name := minerToNodeName(m.addr)
		if name == "" {
			continue
		}
		groupBNames = append(groupBNames, name)
		groupBPower += m.pct
	}

	// Disconnect and block group A nodes from group B nodes (and vice versa)
	var allSavedPeers []peer.AddrInfo
	var blocked []blockedPeer
	totalDisconnected := 0

	// Build peer ID lists for each group
	groupBPeerIDs := map[peer.ID]bool{}
	var groupBPeerList []peer.ID
	for _, name := range groupBNames {
		addrInfo, err := nodes[name].NetAddrsListen(ctx)
		if err == nil {
			groupBPeerIDs[addrInfo.ID] = true
			groupBPeerList = append(groupBPeerList, addrInfo.ID)
		}
	}

	groupAPeerIDs := map[peer.ID]bool{}
	var groupAPeerList []peer.ID
	for _, name := range groupANames {
		addrInfo, err := nodes[name].NetAddrsListen(ctx)
		if err == nil {
			groupAPeerIDs[addrInfo.ID] = true
			groupAPeerList = append(groupAPeerList, addrInfo.ID)
		}
	}

	// Disconnect + block: A nodes block all B peers
	for _, name := range groupANames {
		peers, err := nodes[name].NetPeers(ctx)
		if err != nil {
			continue
		}
		for _, p := range peers {
			if groupBPeerIDs[p.ID] {
				if err := nodes[name].NetDisconnect(ctx, p.ID); err == nil {
					totalDisconnected++
					allSavedPeers = append(allSavedPeers, p)
				}
			}
		}
		if len(groupBPeerList) > 0 {
			if err := nodes[name].NetBlockAdd(ctx, api.NetBlockList{Peers: groupBPeerList}); err != nil {
				log.Printf("[consensus-test] NetBlockAdd on %s failed: %v", name, err)
			} else {
				for _, pid := range groupBPeerList {
					blocked = append(blocked, blockedPeer{onNode: name, peerID: pid})
				}
			}
		}
	}

	// Disconnect + block: B nodes block all A peers
	for _, name := range groupBNames {
		peers, err := nodes[name].NetPeers(ctx)
		if err != nil {
			continue
		}
		for _, p := range peers {
			if groupAPeerIDs[p.ID] {
				if err := nodes[name].NetDisconnect(ctx, p.ID); err == nil {
					totalDisconnected++
				}
			}
		}
		if len(groupAPeerList) > 0 {
			if err := nodes[name].NetBlockAdd(ctx, api.NetBlockList{Peers: groupAPeerList}); err != nil {
				log.Printf("[consensus-test] NetBlockAdd on %s failed: %v", name, err)
			} else {
				for _, pid := range groupAPeerList {
					blocked = append(blocked, blockedPeer{onNode: name, peerID: pid})
				}
			}
		}
	}

	// Block forest from both groups (prevents gossip bridge between partitions)
	for _, name := range nodeKeys {
		if nodeType(name) != "forest" {
			continue
		}
		forestAddrInfo, err := nodes[name].NetAddrsListen(ctx)
		if err != nil {
			continue
		}
		// Block forest on all miners
		for _, mName := range append(groupANames, groupBNames...) {
			if err := nodes[mName].NetBlockAdd(ctx, api.NetBlockList{Peers: []peer.ID{forestAddrInfo.ID}}); err == nil {
				blocked = append(blocked, blockedPeer{onNode: mName, peerID: forestAddrInfo.ID})
			}
			nodes[mName].NetDisconnect(ctx, forestAddrInfo.ID)
		}
		// Block all miners on forest
		allPeerIDs := append(groupAPeerList, groupBPeerList...)
		if len(allPeerIDs) > 0 {
			if err := nodes[name].NetBlockAdd(ctx, api.NetBlockList{Peers: allPeerIDs}); err == nil {
				for _, pid := range allPeerIDs {
					blocked = append(blocked, blockedPeer{onNode: name, peerID: pid})
				}
			}
		}
	}

	log.Printf("[consensus-test] 50/50-bisection: groupA=%v (%.0f%%) vs groupB=%v (%.0f%%), %d disconnected, %d blocked",
		groupANames, groupAPower, groupBNames, groupBPower, totalDisconnected, len(blocked))

	// For adversary metrics: treat groupA as "adversary" (arbitrary — neither side is honest majority)
	// EC is vulnerable if either side has >= 20% (always true with 50/50)
	// F3 quorum: neither side has > 67%, so no quorum
	return &splitResult{
		strategy:      splitBisection,
		adversaryName: groupANames[0], // use first node of groupA for heal
		adversaryPct:  groupAPower,
		honestPct:     groupBPower,
		honestNode:    groupBNames[0],
		advNode:       nodes[groupANames[0]],
		savedPeers:    allSavedPeers,
		blocked:       blocked,
		ecVulnerable:  true, // always true in 50/50
		f3HasQuorum:   false, // neither side has > 67%
		expected:      "EC VULNERABLE, F3 VULNERABLE — no majority on either side",
	}
}

// healPartition removes blocklist entries and reconnects all nodes.
func healPartition(sr *splitResult) {
	// Step 1: Remove all blocklist entries added during partition
	// Group by node to batch removals
	nodeBlocked := map[string][]peer.ID{}
	for _, bp := range sr.blocked {
		nodeBlocked[bp.onNode] = append(nodeBlocked[bp.onNode], bp.peerID)
	}
	for nodeName, peerIDs := range nodeBlocked {
		if err := nodes[nodeName].NetBlockRemove(ctx, api.NetBlockList{Peers: peerIDs}); err != nil {
			log.Printf("[consensus-test] NetBlockRemove on %s failed: %v", nodeName, err)
		}
	}

	// Step 2: Reconnect all nodes (full mesh)
	allAddrs := collectNodeAddrInfos("")
	for _, name := range nodeKeys {
		for _, addr := range allAddrs {
			nodes[name].NetConnect(ctx, addr)
		}
	}
}

// ---------------------------------------------------------------------------
// Attack Injection (rotates across cycles)
// ---------------------------------------------------------------------------

func injectAttack(attack attackType, honestName, advName string, advNode api.FullNode) *attackResult {
	fromAddr, fromKI := pickWallet()
	nonce := nonces[fromAddr]

	// Snapshot sender balance before attack for economic verification
	var preBalance abi.TokenAmount
	actor, err := nodes[honestName].StateGetActor(ctx, fromAddr, types.EmptyTSK)
	if err == nil && actor != nil {
		preBalance = actor.Balance
	}

	var cidA, cidB cid.Cid
	var okA, okB bool
	var attackAmount abi.TokenAmount

	switch attack {
	case attackDoubleSpend:
		// Same nonce, different recipients — classic double-spend
		toA, _ := pickWallet()
		toB, _ := pickWallet()
		for fromAddr == toA || fromAddr == toB || toA == toB {
			toA, _ = pickWallet()
			toB, _ = pickWallet()
		}

		attackAmount = abi.NewTokenAmount(1_000_000_000)
		msgA := baseMsg(fromAddr, toA, attackAmount)
		estimateGas(nodes[honestName], msgA, "test-honest")
		cidA, okA = pushMsgManualNonce(nodes[honestName], msgA, fromKI, nonce, "test-honest")

		msgB := baseMsg(fromAddr, toB, attackAmount)
		estimateGas(advNode, msgB, "test-adversary")
		cidB, okB = pushMsgManualNonce(advNode, msgB, fromKI, nonce, "test-adversary")

		log.Printf("[consensus-test] ATTACK: double-spend")
		log.Printf("[consensus-test]   tx A (honest):    %s → recipient A via %s (gas=%d feecap=%s prem=%s)",
			cidStr(cidA), honestName, msgA.GasLimit, msgA.GasFeeCap, msgA.GasPremium)
		log.Printf("[consensus-test]   tx B (adversary):  %s → recipient B via %s (gas=%d feecap=%s prem=%s)",
			cidStr(cidB), advName, msgB.GasLimit, msgB.GasFeeCap, msgB.GasPremium)

	case attackGasPremiumFrontrun:
		// Same nonce, same recipient, different gas premiums.
		// Estimate gas first for viable GasFeeCap/GasLimit, then set divergent premiums.
		toAddr, _ := pickWallet()
		for fromAddr == toAddr {
			toAddr, _ = pickWallet()
		}

		attackAmount = abi.NewTokenAmount(1_000_000_000)
		msgLow := baseMsg(fromAddr, toAddr, attackAmount)
		estimateGas(nodes[honestName], msgLow, "test-lowfee")
		// Keep estimated GasLimit/GasFeeCap but set a low premium
		msgLow.GasPremium = abi.NewTokenAmount(100)
		cidA, okA = pushMsgManualNonce(nodes[honestName], msgLow, fromKI, nonce, "test-lowfee")

		msgHigh := baseMsg(fromAddr, toAddr, attackAmount)
		estimateGas(advNode, msgHigh, "test-highfee")
		// Keep estimated GasLimit/GasFeeCap but set a high premium
		msgHigh.GasPremium = abi.NewTokenAmount(50_000)
		cidB, okB = pushMsgManualNonce(advNode, msgHigh, fromKI, nonce, "test-highfee")

		log.Printf("[consensus-test] ATTACK: gas-premium-frontrun")
		log.Printf("[consensus-test]   tx A (low fee):   %s premium=%s feecap=%s via %s",
			cidStr(cidA), msgLow.GasPremium, msgLow.GasFeeCap, honestName)
		log.Printf("[consensus-test]   tx B (high fee):  %s premium=%s feecap=%s via %s",
			cidStr(cidB), msgHigh.GasPremium, msgHigh.GasFeeCap, advName)

	case attackBalanceExhaustion:
		// Same nonce, full balance to different recipients
		toA, _ := pickWallet()
		toB, _ := pickWallet()
		for fromAddr == toA || fromAddr == toB || toA == toB {
			toA, _ = pickWallet()
			toB, _ = pickWallet()
		}

		// Query balance (use fresh query; preBalance snapshot may be stale)
		drainActor, drainErr := nodes[honestName].StateGetActor(ctx, fromAddr, types.EmptyTSK)
		if drainErr != nil || drainActor == nil || drainActor.Balance.IsZero() {
			log.Printf("[consensus-test] cannot query balance for %s", fromAddr)
			return nil
		}

		// Reserve 1 FIL for gas
		gasBudget := abi.NewTokenAmount(1_000_000_000_000_000_000)
		if drainActor.Balance.LessThanEqual(gasBudget) {
			log.Printf("[consensus-test] insufficient balance for drain (%s)", drainActor.Balance)
			return nil
		}
		drainAmt := abi.TokenAmount{Int: new(big.Int).Sub(drainActor.Balance.Int, gasBudget.Int)}
		attackAmount = drainAmt

		msgA := baseMsg(fromAddr, toA, drainAmt)
		estimateGas(nodes[honestName], msgA, "test-drain-honest")
		cidA, okA = pushMsgManualNonce(nodes[honestName], msgA, fromKI, nonce, "test-drain-honest")

		msgB := baseMsg(fromAddr, toB, drainAmt)
		estimateGas(advNode, msgB, "test-drain-adv")
		cidB, okB = pushMsgManualNonce(advNode, msgB, fromKI, nonce, "test-drain-adv")

		log.Printf("[consensus-test] ATTACK: balance-exhaustion")
		log.Printf("[consensus-test]   tx A (drain→A):   %s amount=%s via %s", cidStr(cidA), drainAmt, honestName)
		log.Printf("[consensus-test]   tx B (drain→B):   %s amount=%s via %s", cidStr(cidB), drainAmt, advName)
	}

	if !okA || !okB {
		log.Printf("[consensus-test] push failed (okA=%v okB=%v)", okA, okB)
		return nil
	}

	nonces[fromAddr]++

	log.Printf("[consensus-test]   from=%s nonce=%d", fromAddr, nonce)

	return &attackResult{
		attack:     attack,
		fromAddr:   fromAddr,
		nonce:      nonce,
		cidA:       cidA,
		cidB:       cidB,
		honestNode: honestName,
		advNode:    advName,
		amount:     attackAmount,
		preBalance: preBalance,
	}
}

// ---------------------------------------------------------------------------
// Verification
// ---------------------------------------------------------------------------

func verifyOutcome(refNode api.FullNode, ar *attackResult, cycleNum int,
	sr *splitResult, f3Active bool, attack attackType) int {

	log.Printf("[consensus-test] verifying outcome...")

	// Wait for finalized height to advance so txs have time to be included
	// and finalized. More reliable than a fixed sleep.
	waitForFinalizedAdvance(refNode, 5, 2*time.Minute)

	finalA, _ := refNode.StateSearchMsg(ctx, types.EmptyTSK, ar.cidA, 200, false)
	finalB, _ := refNode.StateSearchMsg(ctx, types.EmptyTSK, ar.cidB, 200, false)

	aLanded := finalA != nil && finalA.Receipt.ExitCode.IsSuccess()
	bLanded := finalB != nil && finalB.Receipt.ExitCode.IsSuccess()

	if finalA != nil && !finalA.Receipt.ExitCode.IsSuccess() {
		log.Printf("[consensus-test]   tx A included but reverted (exit=%d)", finalA.Receipt.ExitCode)
	}
	if finalB != nil && !finalB.Receipt.ExitCode.IsSuccess() {
		log.Printf("[consensus-test]   tx B included but reverted (exit=%d)", finalB.Receipt.ExitCode)
	}
	landed := 0
	if aLanded {
		landed++
	}
	if bLanded {
		landed++
	}

	log.Printf("[consensus-test]   tx A (honest):    landed=%v %s", aLanded, fmtHeight(finalA))
	log.Printf("[consensus-test]   tx B (adversary): landed=%v %s", bLanded, fmtHeight(finalB))
	log.Printf("[consensus-test]   total: %d/2", landed)

	details := map[string]any{
		"cycle":         cycleNum,
		"strategy":      sr.strategy.String(),
		"attack":        attack.String(),
		"adversary":     sr.adversaryName,
		"adversary_pct": sr.adversaryPct,
		"honest_pct":    sr.honestPct,
		"f3_active":     f3Active,
		"f3_has_quorum": sr.f3HasQuorum,
		"ec_vulnerable": sr.ecVulnerable,
		"a_landed":      aLanded,
		"b_landed":      bLanded,
		"total_landed":  landed,
		"expected":      sr.expected,
	}

	if sr.f3HasQuorum {
		safe := landed <= 1
		assert.Always(safe, fmt.Sprintf("Consensus: F3 quorum prevents %s", attack), details)
		if !safe {
			log.Printf("[consensus-test] FAIL: %s succeeded despite F3 quorum", attack)
		}
	} else if !sr.ecVulnerable {
		safe := landed <= 1
		assert.Always(safe, fmt.Sprintf("Consensus: EC safe prevents %s", attack), details)
		if !safe {
			log.Printf("[consensus-test] FAIL: %s succeeded despite EC being safe", attack)
		}
	} else {
		if landed == 2 {
			log.Printf("[consensus-test] CONFIRMED: %s succeeded (EC-only, %.1f%% adversary)", attack, sr.adversaryPct)
			assert.Sometimes(true, fmt.Sprintf("%s succeeded under EC-only", attack), details)
		} else if landed == 1 {
			log.Printf("[consensus-test] EC resolved fork — honest majority won")
			assert.Sometimes(true, "EC resolved fork correctly", details)
		} else {
			log.Printf("[consensus-test] Neither tx landed under EC-vulnerable partition")
			assert.Sometimes(landed > 0, "Attack landed at least one tx under EC-vulnerable partition", details)
		}
	}

	return landed
}

// ---------------------------------------------------------------------------
// Economic Impact Verification
// ---------------------------------------------------------------------------

// verifyEconomicImpact checks that the sender's balance decreased by at most
// one transaction's worth. This is the economic proof that a double-spend was
// prevented (or, when EC is vulnerable, that it actually moved excess funds).
func verifyEconomicImpact(refNode api.FullNode, ar *attackResult, sr *splitResult, landed int) {
	if ar.preBalance.IsZero() || ar.amount.IsZero() {
		return // no snapshot available
	}

	finHeight, finTsk := getFinalizedHeight()
	if finHeight < finalizedMinHeight {
		return
	}

	actor, err := refNode.StateGetActor(ctx, ar.fromAddr, finTsk)
	if err != nil || actor == nil {
		log.Printf("[consensus-test] cannot query post-attack balance: %v", err)
		return
	}

	postBalance := actor.Balance
	balanceDrop := new(big.Int).Sub(ar.preBalance.Int, postBalance.Int)

	// Max cost of ONE tx: amount + gasLimit(1M) * gasFeeCap(100K) = ~100B attoFIL
	maxGasCost := big.NewInt(100_000_000_000)
	maxSingleTxCost := new(big.Int).Add(ar.amount.Int, maxGasCost)

	details := map[string]any{
		"from":          ar.fromAddr.String(),
		"pre_balance":   ar.preBalance.String(),
		"post_balance":  postBalance.String(),
		"balance_drop":  balanceDrop.String(),
		"max_single_tx": maxSingleTxCost.String(),
		"amount":        ar.amount.String(),
		"landed":        landed,
		"f3_has_quorum": sr.f3HasQuorum,
		"ec_vulnerable": sr.ecVulnerable,
	}

	if sr.f3HasQuorum || !sr.ecVulnerable {
		// Safety: balance should not drop more than one tx's worth
		safe := balanceDrop.Cmp(maxSingleTxCost) <= 0
		assert.Always(safe, "No double-spend: balance bounded by single tx cost", details)
		if !safe {
			log.Printf("[consensus-test] ECONOMIC VIOLATION: balance dropped by %s, max expected %s",
				balanceDrop, maxSingleTxCost)
		}
	} else {
		// EC-vulnerable: check whether double-spend actually moved excess money
		doubleSpent := balanceDrop.Cmp(maxSingleTxCost) > 0
		if doubleSpent {
			log.Printf("[consensus-test] ECONOMIC CONFIRMATION: double-spend moved excess funds (drop=%s > max=%s)",
				balanceDrop, maxSingleTxCost)
			assert.Sometimes(true, "EC vulnerability: double-spend economic impact confirmed", details)
		}
	}

	log.Printf("[consensus-test] balance: pre=%s post=%s drop=%s max_single=%s",
		ar.preBalance, postBalance, balanceDrop, maxSingleTxCost)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

type attackMineResult struct {
	honestMined bool
	advMined    bool
}

// waitForAttackMined polls both sides of the partition to check if the attack
// transactions have been included in blocks. Returns when both are mined or timeout.
func waitForAttackMined(ar *attackResult, sr *splitResult) attackMineResult {
	deadline := time.After(attackMineTimeout)
	result := attackMineResult{}
	log.Printf("[consensus-test] waiting for attack txs to be mined (timeout=%v)...", attackMineTimeout)

	for {
		select {
		case <-ctx.Done():
			return result
		case <-deadline:
			log.Printf("[consensus-test] attack mine timeout — proceeding with heal")
			return result
		case <-time.After(4 * time.Second):
			if !result.honestMined {
				r, _ := nodes[ar.honestNode].StateSearchMsg(ctx, types.EmptyTSK, ar.cidA, 50, false)
				if r != nil {
					result.honestMined = true
					log.Printf("[consensus-test]   tx A mined on honest side at height %d", r.Height)
				}
			}
			if !result.advMined {
				r, _ := sr.advNode.StateSearchMsg(ctx, types.EmptyTSK, ar.cidB, 50, false)
				if r != nil {
					result.advMined = true
					log.Printf("[consensus-test]   tx B mined on adversary side at height %d", r.Height)
				}
			}
			if result.honestMined && result.advMined {
				log.Printf("[consensus-test] both attack txs mined — healing partition")
				return result
			}
		}
	}
}

func pickHonestNode(adversaryName string) string {
	for _, name := range nodeKeys {
		if name != adversaryName && nodeType(name) == "lotus" {
			return name
		}
	}
	return ""
}

func waitForDivergence(honestName, advName string, advNode api.FullNode) {
	log.Printf("[consensus-test] waiting for divergence (need %d epoch diff OR tipset fork)...", divergeMinEpochs)

	deadline := time.After(divergeTimeout)
	for {
		select {
		case <-ctx.Done():
			return
		case <-deadline:
			log.Printf("[consensus-test] divergence timeout — proceeding anyway")
			return
		case <-time.After(10 * time.Second):
			hHead, e1 := nodes[honestName].ChainHead(ctx)
			aHead, e2 := advNode.ChainHead(ctx)
			if e1 != nil || e2 != nil {
				continue
			}

			// Check height difference
			diff := hHead.Height() - aHead.Height()
			if diff < 0 {
				diff = -diff
			}

			// Check actual tipset fork at the lower height (more reliable than height diff)
			minH := hHead.Height()
			if aHead.Height() < minH {
				minH = aHead.Height()
			}
			forked := false
			if minH > 0 {
				hTs, e1 := nodes[honestName].ChainGetTipSetByHeight(ctx, minH, hHead.Key())
				aTs, e2 := advNode.ChainGetTipSetByHeight(ctx, minH, aHead.Key())
				if e1 == nil && e2 == nil {
					forked = hTs.Key() != aTs.Key()
				}
			}

			log.Printf("[consensus-test]   %s=%d  %s=%d  diff=%d forked=%v",
				honestName, hHead.Height(), advName, aHead.Height(), diff, forked)

			if diff >= divergeMinEpochs || forked {
				log.Printf("[consensus-test] chains diverged (diff=%d, forked=%v)", diff, forked)
				assert.Reachable("Partition achieved chain divergence", map[string]any{
					"honest": honestName, "adversary": advName, "diff": diff, "forked": forked,
				})
				return
			}
		}
	}
}

func waitForSettlement(refNode api.FullNode, preHeight abi.ChainEpoch) {
	log.Printf("[consensus-test] waiting for settlement (%d epochs)...", convergenceBuffer)
	deadline := time.After(settlementTimeout)
	for {
		select {
		case <-ctx.Done():
			return
		case <-deadline:
			return
		case <-time.After(10 * time.Second):
			head, err := refNode.ChainHead(ctx)
			if err != nil {
				continue
			}
			if head.Height() >= preHeight+abi.ChainEpoch(convergenceBuffer)+10 {
				return
			}
		}
	}
}


// waitForFinalizedAdvance waits until the finalized tipset advances by at least
// `epochs` epochs from its current height, or until the timeout expires.
func waitForFinalizedAdvance(node api.FullNode, epochs int, timeout time.Duration) {
	startTs, err := node.ChainGetFinalizedTipSet(ctx)
	if err != nil {
		log.Printf("[consensus-test] cannot get finalized tipset, falling back to sleep")
		time.Sleep(30 * time.Second)
		return
	}

	target := startTs.Height() + abi.ChainEpoch(epochs)
	deadline := time.After(timeout)

	for {
		select {
		case <-ctx.Done():
			return
		case <-deadline:
			log.Printf("[consensus-test] finalized advance timeout (target=%d)", target)
			return
		case <-time.After(5 * time.Second):
			ts, err := node.ChainGetFinalizedTipSet(ctx)
			if err != nil {
				continue
			}
			if ts.Height() >= target {
				log.Printf("[consensus-test] finalized height advanced to %d (target was %d)", ts.Height(), target)
				return
			}
		}
	}
}

func fmtHeight(result *api.MsgLookup) string {
	if result == nil {
		return ""
	}
	return fmt.Sprintf("(height=%d)", result.Height)
}
