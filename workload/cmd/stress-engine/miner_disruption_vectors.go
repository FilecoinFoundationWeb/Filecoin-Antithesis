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
	slashMaxLookback = 15 // tipsets to search for a block by the target miner (from finalized height)
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

	var totalPower int64
	var table []minerPowerInfo

	entries, err := node.F3GetECPowerTable(ctx, types.EmptyTSK)
	if err == nil {
		// F3 power table available
		for _, e := range entries {
			addr, err := address.NewIDAddress(uint64(e.ID))
			if err != nil {
				continue
			}
			p := e.Power.Int64()
			totalPower += p
			table = append(table, minerPowerInfo{addr: addr, power: p})
		}
	} else {
		// F3 unavailable — fall back to StateListMiners + StateMinerPower
		debugLog("[power] F3GetECPowerTable failed (%v), falling back to state power", err)
		miners, err := node.StateListMiners(ctx, types.EmptyTSK)
		if err != nil {
			log.Printf("[power] StateListMiners failed: %v", err)
			return nil
		}
		for _, addr := range miners {
			mp, err := node.StateMinerPower(ctx, addr, types.EmptyTSK)
			if err != nil || mp == nil || mp.MinerPower.QualityAdjPower.IsZero() {
				continue
			}
			p := mp.MinerPower.QualityAdjPower.Int64()
			totalPower += p
			table = append(table, minerPowerInfo{addr: addr, power: p})
		}
	}

	slashedMinersMu.Lock()
	defer slashedMinersMu.Unlock()

	// Filter slashed miners
	filtered := table[:0]
	for _, m := range table {
		if !slashedMiners[m.addr] {
			filtered = append(filtered, m)
		} else {
			totalPower -= m.power
		}
	}
	table = filtered

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

	// Wait for on-chain confirmation before marking slashed.
	// Without this, a failed tx leaves the power table filter thinking the
	// miner is gone, causing n-split to miscalculate adversary percentages.
	result := waitForMsg(lotusNode, msgCid, "power-slash")
	if result == nil {
		log.Printf("[power-slash] slash tx %s did not land on-chain for %s", cidStr(msgCid), target)
		return false
	}
	if result.Receipt.ExitCode != 0 {
		log.Printf("[power-slash] slash tx %s reverted (exit=%d) for %s", cidStr(msgCid), result.Receipt.ExitCode, target)
		return false
	}

	slashedMinersMu.Lock()
	slashedMiners[target] = true
	slashedMinersMu.Unlock()

	// Invalidate power cache so next query reflects the slash
	powerCacheMu.Lock()
	powerCacheEpoch = 0
	powerCacheMu.Unlock()

	log.Printf("[power-slash] slash confirmed for %s (cid=%s, height=%d)", target, cidStr(msgCid), result.Height)
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
	// Start from finalized tipset, not chain head. ReportConsensusFault
	// requires the fault epoch to be at or below the finalized height.
	ts, err := node.ChainGetFinalizedTipSet(ctx)
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

var slashFired bool

func DoPowerAwareSlash() {
	// Only slash once per simulation — repeated slashing kills the network.
	// One slash shifts the power table; nsplit vectors observe the new posture.
	if slashFired {
		return
	}

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

	// No quorum guard — slashing in production doesn't check whether F3
	// quorum survives. If this slash breaks quorum, the nsplit framework
	// detects the changed security posture (f3HasQuorum flips, assertions
	// become Sometimes instead of Always). This tests the real cascade:
	// slash → quorum loss → EC-only vulnerability.
	remaining := remainingPowerPct(table, target.addr)
	breaksQuorum := remaining < quorumThreshold

	log.Printf("[power-slash] strategy=%s target=%s (%.1f%%), remaining=%.1f%% (breaks_quorum=%v)",
		strategy, target.addr, target.pct, remaining, breaksQuorum)

	// Fire-and-forget: submit the slash and return immediately so the deck
	// keeps spinning. The power table updates asynchronously when the tx lands.
	// DoF3FinalityMonitor and nsplit vectors will observe the changed posture.
	slashFired = true

	if !submitConsensusFault(lotusNode, lotusName, target.addr) {
		log.Printf("[power-slash] slash submission failed for %s, will not retry", target.addr)
		return
	}

	log.Printf("[power-slash] slash landed for %s (%.1f%%), remaining=%.1f%% (breaks_quorum=%v) — power table will update",
		target.addr, target.pct, remaining, breaksQuorum)
}

// ===========================================================================
// DoF3FinalityMonitor — Passive F3 Health Check (stateful, non-blocking)
// ===========================================================================

func DoF3FinalityMonitor() {
	// Pick a lotus node — per-node tracking avoids false regressions from
	// querying different nodes that are at different F3 instances.
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

	// Cross-node consistency: skip during partitions — F3 instance spread is
	// expected when nodes are isolated. Post-heal checks verify recovery.
	if partitionActive.Load() {
		return
	}

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
