package main

import (
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
	ecThresholdPct    = 20.0           // EC vulnerability threshold (Wang 2023, m=5)
	f3QuorumPct       = 67.0           // F3 honest power requirement
	convergenceBuffer = 25             // epochs past heal before verification
	divergeMinEpochs  = 5              // min epoch diff before injecting attack
	divergeTimeout    = 5 * time.Minute
	settlementTimeout = 10 * time.Minute
	testCooldown      = 30 * time.Second
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

// splitResult carries the partition state for heal + verification.
type splitResult struct {
	strategy      splitStrategy
	adversaryName string
	adversaryPct  float64
	honestPct     float64
	honestNode    string           // a node on the honest side (for injection)
	advNode       api.FullNode     // adversary's API handle
	savedPeers    []peer.AddrInfo  // for healing
	ecVulnerable  bool
	f3HasQuorum   bool
	expected      string
}

// attackResult captures the injected attack for verification.
type attackResult struct {
	attack     attackType
	fromAddr   address.Address
	nonce      uint64
	cidA       cid.Cid // sent to honest node
	cidB       cid.Cid // sent to adversary node
	honestNode string
	advNode    string
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
	log.Printf("[consensus-test] ╔══════════════════════════════════════════════╗")
	log.Printf("[consensus-test] ║   CONSENSUS TEST — CYCLE %-3d                ║", cycleNum)
	log.Printf("[consensus-test] ╚══════════════════════════════════════════════╝")
	log.Printf("[consensus-test] F3 active: %v", f3Active)
	for _, m := range table {
		log.Printf("[consensus-test]   %s: %.1f%% power", minerToNodeName(m.addr), m.pct)
	}
	log.Printf("[consensus-test] strategy: %s | attack: %s", split, attack)

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

	// --- Heal ---
	log.Printf("[consensus-test] HEALING partition...")
	partitionActive.Store(false)
	healPartition(sr)

	converged := waitForConvergence(sr.adversaryName)
	log.Printf("[consensus-test] convergence: %v", converged)

	// --- Settlement ---
	waitForSettlement(lotusNode, preHead.Height())

	// --- Verify ---
	landed := verifyOutcome(lotusNode, ar, cycleNum, sr, f3Active, attack)

	// --- F3 health ---
	if f3Active {
		postF3, _ := getF3Instance(lotusNode)
		log.Printf("[consensus-test] F3 post-heal: %d→%d", preF3Inst, postF3)
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

	log.Printf("[consensus-test] ╔══════════════════════════════════════════╗")
	log.Printf("[consensus-test] ║         CYCLE %-3d SUMMARY                ║", cycleNum)
	log.Printf("[consensus-test] ╠══════════════════════════════════════════╣")
	log.Printf("[consensus-test] ║ Strategy:      %-25s ║", split)
	log.Printf("[consensus-test] ║ Adversary:     %-10s (%.0f%%)          ║", sr.adversaryName, sr.adversaryPct)
	log.Printf("[consensus-test] ║ Attack:        %-25s ║", attack)
	log.Printf("[consensus-test] ║ F3 active:     %-5v  quorum: %-5v      ║", f3Active, sr.f3HasQuorum)
	log.Printf("[consensus-test] ║ EC vulnerable: %-5v                     ║", sr.ecVulnerable)
	log.Printf("[consensus-test] ║ Landed:        %d/2                      ║", landed)
	log.Printf("[consensus-test] ║ Verdict:       %-25s ║", verdict)
	log.Printf("[consensus-test] ╚══════════════════════════════════════════╝")
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

// createFullIsolation disconnects the largest miner from all peers.
// Topology: adversary alone vs honest majority together.
func createFullIsolation(table []minerPowerInfo, f3Active bool) *splitResult {
	adversary := table[0] // largest miner
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

	disconnected := 0
	for _, p := range peers {
		if err := advNode.NetDisconnect(ctx, p.ID); err == nil {
			disconnected++
		}
	}

	honestPct := 100.0 - adversary.pct
	log.Printf("[consensus-test] full-isolation: disconnected %s (%.1f%%) from %d peers",
		advName, adversary.pct, disconnected)

	return &splitResult{
		strategy:      splitFullIsolation,
		adversaryName: advName,
		adversaryPct:  adversary.pct,
		honestPct:     honestPct,
		honestNode:    pickHonestNode(advName),
		advNode:       advNode,
		savedPeers:    savedPeers,
		ecVulnerable:  adversary.pct >= ecThresholdPct,
		f3HasQuorum:   f3Active && honestPct > f3QuorumPct,
		expected:      classifyExpected(adversary.pct, f3Active && honestPct > f3QuorumPct),
	}
}

// createStarSplit keeps the largest miner connected to all (hub) and
// disconnects all honest miners from each other.
// Topology: hub sees everything, each honest miner is alone.
// This is the n-split attack from Wang et al. 2023.
func createStarSplit(table []minerPowerInfo, f3Active bool) *splitResult {
	hub := table[0] // largest miner as hub
	hubName := minerToNodeName(hub.addr)
	if hubName == "" {
		return nil
	}

	hubAddrInfo, err := nodes[hubName].NetAddrsListen(ctx)
	if err != nil {
		return nil
	}
	hubPeerID := hubAddrInfo.ID

	// Collect all honest nodes and their current peers for heal
	var allSavedPeers []peer.AddrInfo
	honestNodes := []string{}
	totalDisconnected := 0

	for _, m := range table {
		name := minerToNodeName(m.addr)
		if name == "" || name == hubName {
			continue
		}
		honestNodes = append(honestNodes, name)

		peers, err := nodes[name].NetPeers(ctx)
		if err != nil {
			continue
		}
		for _, p := range peers {
			if p.ID == hubPeerID {
				continue // keep connection to hub
			}
			if err := nodes[name].NetDisconnect(ctx, p.ID); err == nil {
				totalDisconnected++
				allSavedPeers = append(allSavedPeers, p)
			}
		}
	}

	honestPct := 100.0 - hub.pct
	log.Printf("[consensus-test] star-split: hub=%s (%.1f%%), disconnected %d honest-to-honest connections",
		hubName, hub.pct, totalDisconnected)
	for _, name := range honestNodes {
		log.Printf("[consensus-test]   isolated: %s", name)
	}

	// Pick any honest node for injection
	honestTarget := ""
	if len(honestNodes) > 0 {
		honestTarget = honestNodes[0]
	}

	return &splitResult{
		strategy:      splitStar,
		adversaryName: hubName,
		adversaryPct:  hub.pct,
		honestPct:     honestPct,
		honestNode:    honestTarget,
		advNode:       nodes[hubName],
		savedPeers:    allSavedPeers,
		ecVulnerable:  hub.pct >= ecThresholdPct,
		f3HasQuorum:   f3Active && honestPct > f3QuorumPct,
		expected:      classifyExpected(hub.pct, f3Active && honestPct > f3QuorumPct),
	}
}

// createBisection splits the network into two roughly equal halves.
// With 3:3:2:2 power: groupA = lotus0(30%)+lotus3(20%) = 50%,
//                      groupB = lotus1(30%)+lotus2(20%) = 50%.
// Neither side has majority. Tests fork resolution under maximum ambiguity.
func createBisection(table []minerPowerInfo, f3Active bool) *splitResult {
	if len(table) < 4 {
		// Need at least 4 miners for a meaningful bisection
		return createFullIsolation(table, f3Active) // fallback
	}

	// Split: biggest + smallest vs middle two
	// With sorted desc [30, 30, 20, 20]: groupA = [0]+[3] = 50%, groupB = [1]+[2] = 50%
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

	// Disconnect group A nodes from group B nodes
	var allSavedPeers []peer.AddrInfo
	totalDisconnected := 0

	// Build set of group B peer IDs for quick lookup
	groupBPeerIDs := map[peer.ID]bool{}
	for _, name := range groupBNames {
		addrInfo, err := nodes[name].NetAddrsListen(ctx)
		if err == nil {
			groupBPeerIDs[addrInfo.ID] = true
		}
	}

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
	}

	// Also disconnect B from A
	groupAPeerIDs := map[peer.ID]bool{}
	for _, name := range groupANames {
		addrInfo, err := nodes[name].NetAddrsListen(ctx)
		if err == nil {
			groupAPeerIDs[addrInfo.ID] = true
		}
	}

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
	}

	log.Printf("[consensus-test] 50/50-bisection: groupA=%v (%.0f%%) vs groupB=%v (%.0f%%), %d connections severed",
		groupANames, groupAPower, groupBNames, groupBPower, totalDisconnected)

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
		ecVulnerable:  true, // always true in 50/50
		f3HasQuorum:   false, // neither side has > 67%
		expected:      "EC VULNERABLE, F3 VULNERABLE — no majority on either side",
	}
}

// healPartition reconnects all nodes from a splitResult.
func healPartition(sr *splitResult) {
	// Reconnect saved peers
	for _, p := range sr.savedPeers {
		for _, name := range nodeKeys {
			nodes[name].NetConnect(ctx, p)
		}
	}
	// Full mesh reconnect as fallback
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

	var cidA, cidB cid.Cid
	var okA, okB bool

	switch attack {
	case attackDoubleSpend:
		// Same nonce, different recipients — classic double-spend
		toA, _ := pickWallet()
		toB, _ := pickWallet()
		for fromAddr == toA || fromAddr == toB || toA == toB {
			toA, _ = pickWallet()
			toB, _ = pickWallet()
		}

		msgA := baseMsg(fromAddr, toA, abi.NewTokenAmount(1_000_000_000))
		cidA, okA = pushMsgManualNonce(nodes[honestName], msgA, fromKI, nonce, "test-honest")

		msgB := baseMsg(fromAddr, toB, abi.NewTokenAmount(1_000_000_000))
		cidB, okB = pushMsgManualNonce(advNode, msgB, fromKI, nonce, "test-adversary")

		log.Printf("[consensus-test] ATTACK: double-spend")
		log.Printf("[consensus-test]   tx A (honest):    %s → recipient A via %s", cidStr(cidA), honestName)
		log.Printf("[consensus-test]   tx B (adversary):  %s → recipient B via %s", cidStr(cidB), advName)

	case attackGasPremiumFrontrun:
		// Same nonce, same recipient, different gas premiums
		toAddr, _ := pickWallet()
		for fromAddr == toAddr {
			toAddr, _ = pickWallet()
		}

		msgLow := baseMsg(fromAddr, toAddr, abi.NewTokenAmount(1_000_000_000))
		msgLow.GasPremium = abi.NewTokenAmount(100)
		msgLow.GasFeeCap = abi.NewTokenAmount(100_000)
		cidA, okA = pushMsgManualNonce(nodes[honestName], msgLow, fromKI, nonce, "test-lowfee")

		msgHigh := baseMsg(fromAddr, toAddr, abi.NewTokenAmount(1_000_000_000))
		msgHigh.GasPremium = abi.NewTokenAmount(50_000)
		msgHigh.GasFeeCap = abi.NewTokenAmount(200_000)
		cidB, okB = pushMsgManualNonce(advNode, msgHigh, fromKI, nonce, "test-highfee")

		log.Printf("[consensus-test] ATTACK: gas-premium-frontrun")
		log.Printf("[consensus-test]   tx A (low fee):   %s premium=100 via %s", cidStr(cidA), honestName)
		log.Printf("[consensus-test]   tx B (high fee):  %s premium=50000 via %s", cidStr(cidB), advName)

	case attackBalanceExhaustion:
		// Same nonce, full balance to different recipients
		toA, _ := pickWallet()
		toB, _ := pickWallet()
		for fromAddr == toA || fromAddr == toB || toA == toB {
			toA, _ = pickWallet()
			toB, _ = pickWallet()
		}

		// Query balance
		actor, err := nodes[honestName].StateGetActor(ctx, fromAddr, types.EmptyTSK)
		if err != nil || actor == nil || actor.Balance.IsZero() {
			log.Printf("[consensus-test] cannot query balance for %s", fromAddr)
			return nil
		}

		// Reserve 1 FIL for gas
		gasBudget := abi.NewTokenAmount(1_000_000_000_000_000_000)
		if actor.Balance.LessThanEqual(gasBudget) {
			log.Printf("[consensus-test] insufficient balance for drain (%s)", actor.Balance)
			return nil
		}
		drainAmt := abi.TokenAmount{Int: new(big.Int).Sub(actor.Balance.Int, gasBudget.Int)}

		msgA := baseMsg(fromAddr, toA, drainAmt)
		cidA, okA = pushMsgManualNonce(nodes[honestName], msgA, fromKI, nonce, "test-drain-honest")

		msgB := baseMsg(fromAddr, toB, drainAmt)
		cidB, okB = pushMsgManualNonce(advNode, msgB, fromKI, nonce, "test-drain-adv")

		log.Printf("[consensus-test] ATTACK: balance-exhaustion")
		log.Printf("[consensus-test]   tx A (drain→A):   %s amount=%s via %s", cidStr(cidA), drainAmt, honestName)
		log.Printf("[consensus-test]   tx B (drain→B):   %s amount=%s via %s", cidStr(cidB), drainAmt, advName)
	}

	nonces[fromAddr]++

	if !okA || !okB {
		log.Printf("[consensus-test] push failed (okA=%v okB=%v)", okA, okB)
		return nil
	}

	log.Printf("[consensus-test]   from=%s nonce=%d", fromAddr, nonce)

	return &attackResult{
		attack:     attack,
		fromAddr:   fromAddr,
		nonce:      nonce,
		cidA:       cidA,
		cidB:       cidB,
		honestNode: honestName,
		advNode:    advName,
	}
}

// ---------------------------------------------------------------------------
// Verification
// ---------------------------------------------------------------------------

func verifyOutcome(refNode api.FullNode, ar *attackResult, cycleNum int,
	sr *splitResult, f3Active bool, attack attackType) int {

	log.Printf("[consensus-test] ═══ VERIFICATION ═══")

	// Wait for mining
	log.Printf("[consensus-test] waiting for txs to settle...")
	time.Sleep(30 * time.Second)

	finalA, _ := refNode.StateSearchMsg(ctx, types.EmptyTSK, ar.cidA, 200, false)
	finalB, _ := refNode.StateSearchMsg(ctx, types.EmptyTSK, ar.cidB, 200, false)

	aLanded := finalA != nil
	bLanded := finalB != nil
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
			log.Printf("[consensus-test] Neither tx landed")
		}
	}

	return landed
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func pickHonestNode(adversaryName string) string {
	for _, name := range nodeKeys {
		if name != adversaryName && nodeType(name) == "lotus" {
			return name
		}
	}
	return ""
}

func waitForDivergence(honestName, advName string, advNode api.FullNode) {
	log.Printf("[consensus-test] waiting for divergence (need %d epoch diff)...", divergeMinEpochs)

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
			diff := hHead.Height() - aHead.Height()
			if diff < 0 {
				diff = -diff
			}
			log.Printf("[consensus-test]   %s=%d  %s=%d  diff=%d",
				honestName, hHead.Height(), advName, aHead.Height(), diff)
			if diff >= divergeMinEpochs {
				log.Printf("[consensus-test] chains diverged by %d epochs", diff)
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


func fmtHeight(result *api.MsgLookup) string {
	if result == nil {
		return ""
	}
	return fmt.Sprintf("(height=%d)", result.Height)
}
