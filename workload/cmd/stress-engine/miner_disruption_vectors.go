package main

import (
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/antithesishq/antithesis-sdk-go/assert"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	builtintypes "github.com/filecoin-project/go-state-types/builtin"
	miner15 "github.com/filecoin-project/go-state-types/builtin/v15/miner"
	"github.com/filecoin-project/go-state-types/crypto"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/types"
)

// ===========================================================================
// Constants
// ===========================================================================

const (
	slashMinHeight   = 30
	slashMaxLookback = 5 // tipsets to search for a block by the target miner
	quorumThreshold  = 67.0
)

// ===========================================================================
// Global state
// ===========================================================================

var (
	slashedMiners   = make(map[address.Address]bool)
	slashedMinersMu sync.Mutex
)

// ===========================================================================
// Power table cache
// ===========================================================================

type minerPowerInfo struct {
	addr  address.Address
	power int64
	pct   float64
}

var (
	powerCache      []minerPowerInfo
	powerCacheMu    sync.Mutex
	powerCacheEpoch abi.ChainEpoch
)

// getF3PowerTable returns the F3 power table, cached per-epoch.
func getF3PowerTable(node api.FullNode) []minerPowerInfo {
	head, err := node.ChainHead(ctx)
	if err != nil {
		log.Printf("[power] ChainHead failed: %v", err)
		return nil
	}

	powerCacheMu.Lock()
	defer powerCacheMu.Unlock()

	if head.Height() == powerCacheEpoch && len(powerCache) > 0 {
		return powerCache
	}

	entries, err := node.F3GetECPowerTable(ctx, types.EmptyTSK)
	if err != nil {
		log.Printf("[power] F3GetECPowerTable failed: %v", err)
		return nil
	}

	slashedMinersMu.Lock()
	defer slashedMinersMu.Unlock()

	var totalPower int64
	var table []minerPowerInfo
	for _, e := range entries {
		addr, err := address.NewIDAddress(uint64(e.ID))
		if err != nil {
			continue
		}
		if slashedMiners[addr] {
			continue
		}
		p := e.Power.Int64()
		totalPower += p
		table = append(table, minerPowerInfo{addr: addr, power: p})
	}

	if totalPower == 0 {
		return nil
	}

	for i := range table {
		table[i].pct = float64(table[i].power) * 100.0 / float64(totalPower)
	}

	sort.Slice(table, func(i, j int) bool { return table[i].power > table[j].power })

	powerCache = table
	powerCacheEpoch = head.Height()
	return table
}

// ===========================================================================
// Miner selection helpers
// ===========================================================================

func pickMinerByStrategy(table []minerPowerInfo, strategy string) minerPowerInfo {
	switch strategy {
	case "biggest":
		return table[0]
	case "smallest":
		return table[len(table)-1]
	default: // "weighted"
		var total int64
		for _, m := range table {
			total += m.power
		}
		r := int64(rngIntn(int(total)))
		var cumulative int64
		for _, m := range table {
			cumulative += m.power
			if r < cumulative {
				return m
			}
		}
		return table[0]
	}
}

func remainingPowerPct(table []minerPowerInfo, target address.Address) float64 {
	var total, targetPower int64
	for _, m := range table {
		total += m.power
		if m.addr == target {
			targetPower = m.power
		}
	}
	if total == 0 {
		return 0
	}
	return float64(total-targetPower) * 100.0 / float64(total)
}

// ===========================================================================
// F3 progress monitoring
// ===========================================================================

var (
	f3LastInstance = make(map[string]uint64)
	f3LastCheckAt  = make(map[string]time.Time)
	f3LastCheckMu  sync.Mutex
)

func getF3Instance(node api.FullNode) (uint64, bool) {
	prog, err := node.F3GetProgress(ctx)
	if err != nil {
		debugLog("[f3-monitor] F3GetProgress failed: %v", err)
		return 0, false
	}
	return prog.ID, true
}

func checkF3Advancing(node api.FullNode, preInstance uint64, timeout time.Duration) (bool, uint64) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		inst, ok := getF3Instance(node)
		if ok && inst > preInstance {
			return true, inst
		}
		time.Sleep(5 * time.Second)
	}
	inst, _ := getF3Instance(node)
	return inst > preInstance, inst
}

// ===========================================================================
// Miner-to-node mapping
// ===========================================================================

func minerToNodeName(miner address.Address) string {
	id, err := address.IDFromAddress(miner)
	if err != nil {
		return ""
	}
	if id < 1000 {
		return ""
	}
	name := fmt.Sprintf("lotus%d", id-1000)
	if _, ok := nodes[name]; !ok {
		return ""
	}
	return name
}

// ===========================================================================
// submitConsensusFault — core slashing logic (extracted from old DoConsensusFault)
// ===========================================================================

func submitConsensusFault(lotusNode api.FullNode, lotusName string, target address.Address) bool {
	minerInfo, err := lotusNode.StateMinerInfo(ctx, target, types.EmptyTSK)
	if err != nil {
		log.Printf("[power-slash] StateMinerInfo failed for %s: %v", target, err)
		return false
	}

	originalBlock := findMinerBlock(lotusNode, target, slashMaxLookback)
	if originalBlock == nil {
		log.Printf("[power-slash] no recent block found for miner %s in last %d tipsets", target, slashMaxLookback)
		return false
	}

	log.Printf("[power-slash] found block by %s at height %d, fabricating equivocation", target, originalBlock.Height)

	origBytes, err := originalBlock.Serialize()
	if err != nil {
		log.Printf("[power-slash] serialize original block failed: %v", err)
		return false
	}

	forgedBlock := *originalBlock
	forgedBlock.ForkSignaling = originalBlock.ForkSignaling + 1
	forgedBlock.BlockSig = nil

	forgedSigningBytes, err := forgedBlock.SigningBytes()
	if err != nil {
		log.Printf("[power-slash] SigningBytes failed: %v", err)
		return false
	}

	sig, err := walletSignOnLotusNode(minerInfo.Worker, forgedSigningBytes)
	if err != nil {
		log.Printf("[power-slash] WalletSign failed: %v", err)
		return false
	}
	forgedBlock.BlockSig = sig

	forgedBytes, err := forgedBlock.Serialize()
	if err != nil {
		log.Printf("[power-slash] serialize forged block failed: %v", err)
		return false
	}

	params := miner15.ReportConsensusFaultParams{
		BlockHeader1: origBytes,
		BlockHeader2: forgedBytes,
	}
	serializedParams, err := actors.SerializeParams(&params)
	if err != nil {
		log.Printf("[power-slash] SerializeParams failed: %v", err)
		return false
	}

	reporter, reporterKI := pickWallet()
	msg := &types.Message{
		From:   reporter,
		To:     target,
		Value:  abi.NewTokenAmount(0),
		Method: builtintypes.MethodsMiner.ReportConsensusFault,
		Params: serializedParams,
	}

	log.Printf("[power-slash] submitting ReportConsensusFault against %s from %s via %s", target, reporter, lotusName)

	msgCid, ok := pushContractMsg(lotusNode, msg, reporterKI, "power-slash")
	if !ok {
		return false
	}

	result := waitForMsg(lotusNode, msgCid, "power-slash")
	if result == nil {
		log.Printf("[power-slash] message not confirmed within timeout")
		return false
	}

	if !result.Receipt.ExitCode.IsSuccess() {
		log.Printf("[power-slash] ReportConsensusFault rejected: exit=%d", result.Receipt.ExitCode)
		return false
	}

	slashedMinersMu.Lock()
	slashedMiners[target] = true
	slashedMinersMu.Unlock()

	log.Printf("[power-slash] miner %s successfully slashed!", target)
	return true
}

// ===========================================================================
// Shared node helpers
// ===========================================================================

// pickLotusNode returns a lotus (non-forest) node for operations requiring WalletSign.
func pickLotusNode() (api.FullNode, string) {
	var lotusNodes []string
	for _, name := range nodeKeys {
		if nodeType(name) == "lotus" {
			lotusNodes = append(lotusNodes, name)
		}
	}
	if len(lotusNodes) == 0 {
		return nil, ""
	}
	name := rngChoice(lotusNodes)
	return nodes[name], name
}

// walletSignOnLotusNode tries WalletSign on each lotus node until one succeeds.
func walletSignOnLotusNode(workerAddr address.Address, data []byte) (*crypto.Signature, error) {
	for _, name := range nodeKeys {
		if nodeType(name) != "lotus" {
			continue
		}
		sig, err := nodes[name].WalletSign(ctx, workerAddr, data)
		if err == nil {
			debugLog("[power-slash] signed with worker key on %s", name)
			return sig, nil
		}
		debugLog("[power-slash] WalletSign failed on %s: %v", name, err)
	}
	return nil, fmt.Errorf("no lotus node holds worker key for %s", workerAddr)
}

// findMinerBlock searches recent tipsets for a block produced by the given miner.
func findMinerBlock(node api.FullNode, miner address.Address, maxDepth int) *types.BlockHeader {
	head, err := node.ChainHead(ctx)
	if err != nil {
		return nil
	}

	parentTs, err := node.ChainGetTipSet(ctx, head.Parents())
	if err != nil {
		return nil
	}
	ts, err := node.ChainGetTipSet(ctx, parentTs.Parents())
	if err != nil {
		return nil
	}

	for depth := 0; depth < maxDepth && ts != nil; depth++ {
		for _, blk := range ts.Blocks() {
			if blk.Miner == miner {
				return blk
			}
		}
		ts, err = node.ChainGetTipSet(ctx, ts.Parents())
		if err != nil {
			return nil
		}
	}
	return nil
}

// getEligibleMiners returns miners not yet slashed.
// Returns nil if fewer than 2 eligible (must preserve at least one active miner).
func getEligibleMiners(node api.FullNode) []address.Address {
	miners, err := node.StateListMiners(ctx, types.EmptyTSK)
	if err != nil {
		log.Printf("[power-slash] StateListMiners failed: %v", err)
		return nil
	}

	slashedMinersMu.Lock()
	defer slashedMinersMu.Unlock()

	var eligible []address.Address
	for _, m := range miners {
		if !slashedMiners[m] {
			eligible = append(eligible, m)
		}
	}
	if len(eligible) < 2 {
		debugLog("[power-slash] only %d eligible miners, need >= 2", len(eligible))
		return nil
	}
	return eligible
}

// ===========================================================================
// DoPowerAwareSlash — Power-Targeted Consensus Fault
// ===========================================================================

func DoPowerAwareSlash() {
	if len(nodeKeys) < 2 {
		return
	}

	head, err := nodes[nodeKeys[0]].ChainHead(ctx)
	if err != nil {
		return
	}
	if head.Height() < slashMinHeight {
		return
	}

	lotusNode, lotusName := pickLotusNode()
	if lotusNode == nil {
		return
	}

	table := getF3PowerTable(lotusNode)
	if len(table) < 2 {
		log.Printf("[power-slash] power table too small: %d entries", len(table))
		return
	}

	// Roll strategy
	strategies := []string{"biggest", "smallest", "weighted"}
	strategy := strategies[rngIntn(len(strategies))]
	target := pickMinerByStrategy(table, strategy)

	// Quorum guard: don't slash if remaining power < 67%
	remaining := remainingPowerPct(table, target.addr)
	if remaining < quorumThreshold {
		log.Printf("[power-slash] target %s (%.1f%%), remaining %.1f%% < %.0f%% — skipped (quorum guard)",
			target.addr, target.pct, remaining, quorumThreshold)
		return
	}

	log.Printf("[power-slash] strategy=%s target=%s (%.1f%%), remaining=%.1f%%",
		strategy, target.addr, target.pct, remaining)

	// Snapshot F3 instance before slash
	preInst, f3ok := getF3Instance(lotusNode)

	if !submitConsensusFault(lotusNode, lotusName, target.addr) {
		return
	}

	// Check F3 recovery after slash
	if f3ok {
		advanced, postInst := checkF3Advancing(lotusNode, preInst, 3*time.Minute)

		assert.Sometimes(advanced, "F3 advances after power-aware slash", map[string]any{
			"strategy":      strategy,
			"target":        target.addr.String(),
			"target_pct":    target.pct,
			"remaining_pct": remaining,
			"pre_instance":  preInst,
			"post_instance": postInst,
		})

		if advanced {
			log.Printf("[power-slash] F3 advanced %d→%d after slashing %s", preInst, postInst, target.addr)
		} else {
			log.Printf("[power-slash] F3 did NOT advance after slashing %s (instance stuck at %d)", target.addr, preInst)
		}
	}
}

// ===========================================================================
// DoQuorumBoundaryTest — Deliberate F3 Stall (opt-in, weight 0 by default)
// ===========================================================================

func DoQuorumBoundaryTest() {
	if len(nodeKeys) < 2 {
		return
	}

	head, err := nodes[nodeKeys[0]].ChainHead(ctx)
	if err != nil {
		return
	}
	if head.Height() < slashMinHeight {
		return
	}

	lotusNode, lotusName := pickLotusNode()
	if lotusNode == nil {
		return
	}

	table := getF3PowerTable(lotusNode)
	if len(table) < 3 {
		return
	}

	// Find a miner whose removal drops remaining power below quorum
	var stallTarget *minerPowerInfo
	for i := range table {
		rem := remainingPowerPct(table, table[i].addr)
		if rem < quorumThreshold {
			stallTarget = &table[i]
			break
		}
	}

	if stallTarget == nil {
		debugLog("[quorum-test] no miner large enough to break quorum, skipping")
		return
	}

	remaining := remainingPowerPct(table, stallTarget.addr)
	log.Printf("[quorum-test] slashing %s (%.1f%%), remaining %.1f%% (below %.0f%% quorum)",
		stallTarget.addr, stallTarget.pct, remaining, quorumThreshold)

	preInst, f3ok := getF3Instance(lotusNode)

	if !submitConsensusFault(lotusNode, lotusName, stallTarget.addr) {
		return
	}

	if !f3ok {
		return
	}

	// Phase 1: Check if F3 stalls (expected)
	stalled, inst1 := checkF3Advancing(lotusNode, preInst, 3*time.Minute)

	assert.Sometimes(!stalled, "F3 stalls when remaining power < 2/3", map[string]any{
		"target":        stallTarget.addr.String(),
		"target_pct":    stallTarget.pct,
		"remaining_pct": remaining,
		"pre_instance":  preInst,
		"post_instance": inst1,
	})

	// Phase 2: Check if F3 recovers (power table should update)
	recovered, inst2 := checkF3Advancing(lotusNode, inst1, 5*time.Minute)

	assert.Sometimes(recovered, "F3 eventually recovers after power table update", map[string]any{
		"target":        stallTarget.addr.String(),
		"pre_instance":  preInst,
		"phase1_inst":   inst1,
		"post_instance": inst2,
	})

	if recovered {
		log.Printf("[quorum-test] F3 recovered: %d→%d→%d", preInst, inst1, inst2)
	} else {
		log.Printf("[quorum-test] F3 did NOT recover (stuck at %d)", inst2)
	}
}

// ===========================================================================
// DoF3FinalityMonitor — Passive F3 Health Check (stateful, non-blocking)
// ===========================================================================

func DoF3FinalityMonitor() {
	// Pick a lotus node (forest may not support F3 API)
	lotusNode, nodeName := pickLotusNode()
	if lotusNode == nil {
		return
	}

	f3LastCheckMu.Lock()
	defer f3LastCheckMu.Unlock()

	inst, ok := getF3Instance(lotusNode)
	if !ok {
		return
	}

	// Phase 1: record baseline for this node and return
	if f3LastCheckAt[nodeName].IsZero() {
		f3LastInstance[nodeName] = inst
		f3LastCheckAt[nodeName] = time.Now()
		debugLog("[f3-monitor] baseline recorded: node=%s instance=%d", nodeName, inst)
		return
	}

	// Phase 2: check only if enough time has passed for this node
	if time.Since(f3LastCheckAt[nodeName]) < 15*time.Second {
		return
	}

	prevInst := f3LastInstance[nodeName]
	f3LastInstance[nodeName] = inst
	f3LastCheckAt[nodeName] = time.Now()

	// Safety: F3 instance should never regress on the same node
	assert.Always(inst >= prevInst, "F3 instance never regresses", map[string]any{
		"node":     nodeName,
		"previous": prevInst,
		"current":  inst,
	})

	// Liveness: F3 should be making progress
	advanced := inst > prevInst
	assert.Sometimes(advanced, "F3 is making progress", map[string]any{
		"node":     nodeName,
		"previous": prevInst,
		"current":  inst,
	})

	if advanced {
		debugLog("[f3-monitor] %s F3 instance %d → %d", nodeName, prevInst, inst)
	}

	// Cross-node consistency: check all lotus nodes
	var minInst, maxInst uint64
	first := true
	for _, name := range nodeKeys {
		if nodeType(name) != "lotus" {
			continue
		}
		nodeInst, nodeOk := getF3Instance(nodes[name])
		if !nodeOk {
			continue
		}
		if first {
			minInst, maxInst = nodeInst, nodeInst
			first = false
		}
		if nodeInst < minInst {
			minInst = nodeInst
		}
		if nodeInst > maxInst {
			maxInst = nodeInst
		}
	}

	if !first {
		consistent := maxInst-minInst <= 2
		assert.Sometimes(consistent, "F3 instances consistent across nodes", map[string]any{
			"min_instance": minInst,
			"max_instance": maxInst,
			"spread":       maxInst - minInst,
		})
	}
}
