package main

import (
	"bytes"
	"log"
	"sync"
	"time"

	"github.com/antithesishq/antithesis-sdk-go/assert"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/builtin/v15/eam"
	"github.com/filecoin-project/go-state-types/network"
	"github.com/filecoin-project/lotus/api"
)

// ===========================================================================
// Generic Network Upgrade Test Suite
//
// Upgrade-agnostic vectors that validate state consistency across ANY network
// version transition. All nodes upgrade in lockstep at each configured
// boundary (no partial-migration handling).
//
// Two categories:
//
//   GENERIC — run for every upgrade, no FIP knowledge needed:
//     - Network version agreement across nodes
//     - Per-node upgrade activation
//     - State root agreement at migration epoch±1
//     - Receipt root consistency at boundary
//     - Chain progress across boundary (stall detector)
//     - Boundary-timed message/actor-churn stress
//
//   FIP-SPECIFIC — registered by per-upgrade files (e.g. nv28_vectors.go):
//     - Custom boundary stress functions
//     - Precompile/opcode tests
//     - Gas formula validation
//
// Self-gates per boundary: active in [epoch-10, epoch+30]; silent otherwise.
// Every assertion payload carries `boundary` so the Antithesis report
// distinguishes which upgrade broke.
//
// Known interaction: DoHeightProgression (consensus_vectors.go) tolerates
// <=10-epoch cross-node spread. A slow-migrating impl may briefly exceed
// this during migration. If that causes false positives, widen its
// tolerance only while nearUpgrade is true; do not duplicate the check here.
// ===========================================================================

// ---------------------------------------------------------------------------
// Boundary configuration
// ---------------------------------------------------------------------------

const upgradeBoundaryWindow = 30 // epochs past upgrade before suite goes quiet

type upgradeBoundary struct {
	Name  string // e.g. "NV27", "NV28"
	Epoch abi.ChainEpoch
}

var (
	upgradeOnce        sync.Once
	upgradeBoundaries  []upgradeBoundary
)

func initUpgradeState() {
	upgradeOnce.Do(func() {
		// Only include boundaries with a real mid-test epoch (>0). Negative or
		// zero values mean "already active at genesis" — nothing to test.
		if g := abi.ChainEpoch(envInt("GOLDENWEEK_HEIGHT", 0)); g > 0 {
			upgradeBoundaries = append(upgradeBoundaries, upgradeBoundary{"NV27", g})
		}
		if x := abi.ChainEpoch(envInt("FIREHORSE_HEIGHT", 0)); x > 0 {
			upgradeBoundaries = append(upgradeBoundaries, upgradeBoundary{"NV28", x})
		}
	})
}

// nearUpgrade returns true if height is within the active boundary window.
func nearUpgrade(height abi.ChainEpoch, b upgradeBoundary) bool {
	return height >= b.Epoch-10 && height <= b.Epoch+abi.ChainEpoch(upgradeBoundaryWindow)
}

// ---------------------------------------------------------------------------
// FIP-specific hook: per-upgrade files register boundary stress functions
// ---------------------------------------------------------------------------

// UpgradeBoundaryFunc is a function that runs FIP-specific stress at the
// upgrade boundary. It receives the current chain height and boundary so it
// can self-gate (e.g. only fire for b.Name == "NV28").
type UpgradeBoundaryFunc func(currentHeight abi.ChainEpoch, b upgradeBoundary)

var fipBoundaryFuncs []UpgradeBoundaryFunc

// RegisterFIPBoundaryFunc adds a FIP-specific stress function to the suite.
// Call from init() in per-upgrade files (e.g. nv28_vectors.go).
func RegisterFIPBoundaryFunc(fn UpgradeBoundaryFunc) {
	fipBoundaryFuncs = append(fipBoundaryFuncs, fn)
}

// extractActorID decodes a CreateExternalReturn from a message lookup result.
// Exposed for FIP-specific vectors that deploy actors at the boundary.
func extractActorID(result *api.MsgLookup) *address.Address {
	if result == nil || result.Receipt.Return == nil {
		return nil
	}
	var ret eam.CreateExternalReturn
	if err := ret.UnmarshalCBOR(bytes.NewReader(result.Receipt.Return)); err != nil {
		return nil
	}
	addr, err := address.NewIDAddress(ret.ActorID)
	if err != nil {
		return nil
	}
	return &addr
}

// ---------------------------------------------------------------------------
// DoUpgradeSuite — single deck entry
//
// For each configured boundary, if the current max-head is within the active
// window, run the generic assertions and boundary-timed stress. FIP-specific
// hooks run last, once per invocation (they self-gate internally).
// ---------------------------------------------------------------------------

func DoUpgradeSuite() {
	initUpgradeState()
	if len(upgradeBoundaries) == 0 {
		return
	}

	currentHeight := maxHeadAcrossNodes()
	if currentHeight == 0 {
		return
	}

	for _, b := range upgradeBoundaries {
		if !nearUpgrade(currentHeight, b) {
			continue
		}
		doNetworkVersionAgreement(b)
		doUpgradeActivation(currentHeight, b)
		doMigrationStateRootAgreement(b)
		doReceiptConsistencyAtBoundary(b)
		doChainProgressAcrossBoundary(currentHeight, b)
		doPostUpgradeNodeHealth(currentHeight, b)
		doBoundaryMessageBurst(currentHeight, b)
		doActorChurnAtBoundary(currentHeight, b)
		for _, fn := range fipBoundaryFuncs {
			fn(currentHeight, b)
		}
	}
}

// maxHeadAcrossNodes returns the highest head height seen across all nodes.
// A crashed/restarted node shouldn't suppress the suite for everyone else.
func maxHeadAcrossNodes() abi.ChainEpoch {
	var h abi.ChainEpoch
	for _, name := range nodeKeys {
		head, err := nodes[name].ChainHead(ctx)
		if err != nil {
			continue
		}
		if head.Height() > h {
			h = head.Height()
		}
	}
	return h
}

// ===========================================================================
// Generic Assertions
// ===========================================================================

// doNetworkVersionAgreement — all nodes must report the same NV at a
// finalized height near the boundary.
func doNetworkVersionAgreement(b upgradeBoundary) {
	if len(nodeKeys) < 2 {
		return
	}

	snap := getFinalizedSnapshots()
	finalizedHeight, anchorKey := snapshotMinHeight(snap)
	if finalizedHeight < finalizedMinHeight {
		return
	}

	checkHeight := finalizedHeight - abi.ChainEpoch(rngIntn(5))
	if checkHeight < 1 {
		checkHeight = 1
	}

	versions := make(map[network.Version][]string)
	responded := 0

	for name, s := range snap {
		if s.err != nil {
			continue
		}
		ts, err := nodes[name].ChainGetTipSetByHeight(ctx, checkHeight, anchorKey)
		if err != nil {
			continue
		}
		nv, err := nodes[name].StateNetworkVersion(ctx, ts.Key())
		if err != nil {
			continue
		}
		versions[nv] = append(versions[nv], name)
		responded++
	}

	if responded < 2 {
		return
	}

	agreed := len(versions) == 1

	assert.Always(agreed, "Network version agrees across all nodes", map[string]any{
		"boundary":     b.Name,
		"height":       checkHeight,
		"finalized_at": finalizedHeight,
		"versions":     versions,
		"responded":    responded,
		"near_boundary": nearUpgrade(checkHeight, b),
	})

	if !agreed {
		log.Printf("[upgrade/%s] NETWORK VERSION DIVERGENCE at height %d: %v", b.Name, checkHeight, versions)
	}
}

// doUpgradeActivation — every node's NV must advance across the boundary.
// Catches a node that silently didn't activate the upgrade.
//
// Anchored on the finalized chain: each node's `head.Key()` is unstable under
// reorgs (FIP profile runs DoReorgChaos, and Antithesis fault-injection
// adds further partitions). Walking back from a head that is mid-reorg can
// sample a transient fork whose state hasn't applied the migration yet,
// producing false ACTIVATION DIVERGENCE reports. By gating on a finalized
// anchor we only check chain segments that consensus has agreed on.
func doUpgradeActivation(_ abi.ChainEpoch, b upgradeBoundary) {
	if b.Epoch < 2 {
		return
	}

	snap := getFinalizedSnapshots()
	finalizedHeight, anchorKey := snapshotMinHeight(snap)
	// Require the boundary plus a buffer to be finalized on every responding
	// node before sampling. The +5 buffer keeps the post-side query clear of
	// short null-round runs near the boundary.
	if finalizedHeight < b.Epoch+5 {
		return
	}

	perNode := make(map[string]map[string]network.Version)
	allActivated := true
	checked := 0

	for _, name := range nodeKeys {
		if s, ok := snap[name]; !ok || s.err != nil {
			continue
		}
		n := nodes[name]

		// Sample the post-side at b.Epoch+5 instead of b.Epoch+1.
		// ChainGetTipSetByHeight backfills null rounds by returning the most
		// recent non-null tipset at or below the requested height. With +1 a
		// null round at epoch+1 can drop us back onto the boundary tipset
		// itself (height == b.Epoch), where StateNetworkVersion returns the
		// pre-migration NV (Lotus/Forest both treat the boundary tipset as
		// "applying the migration" — the NV at the start of processing it is
		// the old one). A +5 cushion stays inside the gate above
		// (finalizedHeight >= b.Epoch+5) so the query is always within the
		// finalized chain.
		postTs, err := n.ChainGetTipSetByHeight(ctx, b.Epoch+5, anchorKey)
		if err != nil {
			continue
		}
		// Defensive: if backfill still landed at or before the boundary
		// (e.g. a long null-round run), skip — we can't read post-state
		// reliably here.
		if postTs.Height() <= b.Epoch {
			continue
		}
		postNV, err := n.StateNetworkVersion(ctx, postTs.Key())
		if err != nil {
			continue
		}

		preTs, err := n.ChainGetTipSetByHeight(ctx, b.Epoch-1, anchorKey)
		if err != nil {
			continue
		}
		// Null-skip on the pre side is safe: returned tipset is still
		// pre-boundary, so NV is still pre-upgrade.
		preNV, err := n.StateNetworkVersion(ctx, preTs.Key())
		if err != nil {
			continue
		}

		perNode[name] = map[string]network.Version{"pre": preNV, "post": postNV}
		if postNV <= preNV {
			allActivated = false
		}
		checked++
	}

	if checked < 2 {
		return
	}

	assert.Always(allActivated, "All nodes activated upgrade across boundary", map[string]any{
		"boundary":         b.Name,
		"upgrade_epoch":    b.Epoch,
		"finalized_height": finalizedHeight,
		"per_node":         perNode,
		"checked":          checked,
	})

	if !allActivated {
		log.Printf("[upgrade/%s] ACTIVATION DIVERGENCE at epoch %d (finalized=%d): %v", b.Name, b.Epoch, finalizedHeight, perNode)
	}
}

// doMigrationStateRootAgreement — state roots at epoch-1, epoch, epoch+1 must match.
// Boundary-forced complement to STRESS_WEIGHT_STATE_ROOT which samples random heights.
func doMigrationStateRootAgreement(b upgradeBoundary) {
	if len(nodeKeys) < 2 {
		return
	}

	snap := getFinalizedSnapshots()
	finalizedHeight, anchorKey := snapshotMinHeight(snap)
	if finalizedHeight < b.Epoch+2 {
		return
	}

	for _, offset := range []abi.ChainEpoch{-1, 0, 1} {
		checkHeight := b.Epoch + offset
		if checkHeight < 1 {
			continue
		}

		stateRoots := make(map[string][]string)
		for name, s := range snap {
			if s.err != nil {
				continue
			}
			ts, err := nodes[name].ChainGetTipSetByHeight(ctx, checkHeight, anchorKey)
			if err != nil {
				continue
			}
			root := ts.ParentState().String()
			stateRoots[root] = append(stateRoots[root], name)
		}

		totalResponded := 0
		for _, names := range stateRoots {
			totalResponded += len(names)
		}
		if totalResponded < 2 {
			continue
		}

		agreed := len(stateRoots) == 1

		phase := "at"
		if offset == -1 {
			phase = "before"
		} else if offset == 1 {
			phase = "after"
		}

		assert.Always(agreed, "State root agrees "+phase+" upgrade migration", map[string]any{
			"boundary":      b.Name,
			"height":        checkHeight,
			"upgrade_epoch": b.Epoch,
			"phase":         phase,
			"state_roots":   stateRoots,
			"responded":     totalResponded,
		})

		if !agreed {
			log.Printf("[upgrade/%s] STATE ROOT DIVERGENCE %s migration (epoch %d): %v", b.Name, phase, checkHeight, stateRoots)
		}
	}
}

// doReceiptConsistencyAtBoundary — receipt roots at upgrade+1 must match.
// Complements DoStateAudit (receipt count) and DoReceiptAudit (per-message).
func doReceiptConsistencyAtBoundary(b upgradeBoundary) {
	if len(nodeKeys) < 2 {
		return
	}

	snap := getFinalizedSnapshots()
	finalizedHeight, anchorKey := snapshotMinHeight(snap)
	if finalizedHeight < b.Epoch+2 {
		return
	}

	checkHeight := b.Epoch + 1

	receiptRoots := make(map[string][]string)
	for name, s := range snap {
		if s.err != nil {
			continue
		}
		ts, err := nodes[name].ChainGetTipSetByHeight(ctx, checkHeight, anchorKey)
		if err != nil || len(ts.Blocks()) == 0 {
			continue
		}
		root := ts.Blocks()[0].ParentMessageReceipts.String()
		receiptRoots[root] = append(receiptRoots[root], name)
	}

	totalResponded := 0
	for _, names := range receiptRoots {
		totalResponded += len(names)
	}
	if totalResponded < 2 {
		return
	}

	agreed := len(receiptRoots) == 1

	assert.Always(agreed, "Receipt roots agree at first post-upgrade epoch", map[string]any{
		"boundary":      b.Name,
		"height":        checkHeight,
		"upgrade_epoch": b.Epoch,
		"receipt_roots": receiptRoots,
		"responded":     totalResponded,
	})

	if !agreed {
		log.Printf("[upgrade/%s] RECEIPT DIVERGENCE at post-upgrade epoch %d: %v", b.Name, checkHeight, receiptRoots)
	}
}

// doChainProgressAcrossBoundary — samples max head now and ~30s later in the
// [epoch, epoch+20] window. Catches migration stalls that make all the other
// boundary assertions skip silently (they short-circuit on finalizedHeight
// < epoch+2). DoHeightProgression checks cross-node spread, not time delta.
//
// Uses assert.Sometimes (liveness): Antithesis fault injection includes
// global pauses and time dilation, so a single observation of "no progress
// over 30s wall-clock" is expected under faults and must not fail the run.
// A true migration stall manifests as *no* observation ever seeing progress
// across the whole [epoch, epoch+20] window — that's what Sometimes catches.
func doChainProgressAcrossBoundary(currentHeight abi.ChainEpoch, b upgradeBoundary) {
	if currentHeight < b.Epoch || currentHeight > b.Epoch+20 {
		return
	}

	before := maxHeadAcrossNodes()
	if before == 0 {
		return
	}
	time.Sleep(30 * time.Second)
	after := maxHeadAcrossNodes()
	if after == 0 {
		return
	}

	progressed := after > before

	assert.Sometimes(progressed, "Chain makes progress across upgrade boundary", map[string]any{
		"boundary":      b.Name,
		"upgrade_epoch": b.Epoch,
		"before":        before,
		"after":         after,
		"delta":         int64(after - before),
	})

	if !progressed {
		log.Printf("[upgrade/%s] no progress in 30s window at epoch %d (before=%d, after=%d) — may be fault-injection pause", b.Name, b.Epoch, before, after)
	}
}

// doPostUpgradeNodeHealth — in the post-boundary window, verifies every
// declared node has at some point been observed responsive AND past the
// upgrade epoch. Uses Sometimes (liveness) so that transient Antithesis
// faults (crash/pause/restart of a single node at the moment we query) do
// not false-positive. A node that stays unreachable or stuck below the
// boundary for the *entire* window will fail this assertion.
func doPostUpgradeNodeHealth(currentHeight abi.ChainEpoch, b upgradeBoundary) {
	if currentHeight <= b.Epoch+5 {
		return
	}

	perNode := make(map[string]map[string]any)
	allHealthy := true

	for _, name := range nodeKeys {
		head, err := nodes[name].ChainHead(ctx)
		if err != nil {
			perNode[name] = map[string]any{"responsive": false, "err": err.Error()}
			allHealthy = false
			continue
		}
		pastBoundary := head.Height() > b.Epoch
		perNode[name] = map[string]any{
			"responsive":    true,
			"height":        int64(head.Height()),
			"past_boundary": pastBoundary,
		}
		if !pastBoundary {
			allHealthy = false
		}
	}

	assert.Sometimes(allHealthy, "All nodes responsive and past upgrade boundary", map[string]any{
		"boundary":      b.Name,
		"upgrade_epoch": b.Epoch,
		"per_node":      perNode,
	})
}

// ===========================================================================
// Generic Boundary Stress
// ===========================================================================

// doBoundaryMessageBurst — submits messages right before upgrade so they're
// in-flight when the state migration runs.
func doBoundaryMessageBurst(currentHeight abi.ChainEpoch, b upgradeBoundary) {
	epochsUntilUpgrade := b.Epoch - currentHeight
	if epochsUntilUpgrade < 0 || epochsUntilUpgrade > 5 {
		return
	}

	log.Printf("[upgrade-stress/%s] boundary burst: %d epochs until upgrade", b.Name, epochsUntilUpgrade)

	burstCount := rngIntn(6) + 5
	sent := 0
	for i := 0; i < burstCount; i++ {
		fromAddr, fromKI := pickWallet()
		toAddr, _ := pickWallet()
		if fromAddr == toAddr {
			continue
		}
		nodeName, n := pickNode()
		msg := baseMsg(fromAddr, toAddr, abi.NewTokenAmount(int64(rngIntn(1000)+1)))
		if pushMsg(n, msg, fromKI, "upgrade-burst") {
			sent++
			debugLog("[upgrade-stress/%s] burst msg %d via %s", b.Name, i, nodeName)
		}
	}

	if sent > 0 {
		assert.Reachable("Boundary message burst submitted before upgrade", map[string]any{
			"boundary":      b.Name,
			"sent":          sent,
			"epochs_until":  epochsUntilUpgrade,
			"upgrade_epoch": b.Epoch,
		})
	}

	time.Sleep(15 * time.Second)
	for _, addr := range addrs[:min(5, len(addrs))] {
		verifyActorConsistency(addr, "post-upgrade-burst-"+b.Name)
	}
}

// doActorChurnAtBoundary — burst deploy/destroy around the upgrade epoch
// to stress HAMT during migration.
func doActorChurnAtBoundary(currentHeight abi.ChainEpoch, b upgradeBoundary) {
	distance := currentHeight - b.Epoch
	if distance < 0 {
		distance = -distance
	}
	if distance > 5 {
		return
	}

	log.Printf("[upgrade-stress/%s] actor churn: height=%d upgrade=%d", b.Name, currentHeight, b.Epoch)

	bytecode := contractBytecodes["selfdestruct"]
	if bytecode == nil {
		return
	}

	deployed := 0
	destroyed := 0
	for i := 0; i < 3; i++ {
		fromAddr, fromKI := pickWallet()
		_, n := pickNode()

		msgCid, ok := deployContract(n, fromAddr, fromKI, bytecode, "upgrade-churn-deploy")
		if !ok {
			continue
		}
		result := waitForMsg(n, msgCid, "upgrade-churn-deploy")
		if result == nil || !result.Receipt.ExitCode.IsSuccess() {
			continue
		}
		deployed++

		idAddr := extractActorID(result)
		if idAddr == nil {
			continue
		}

		destroyCalldata, err := cborWrapCalldata(calcSelector("destroy()"))
		if err != nil {
			continue
		}

		destroyCid, ok := invokeContract(n, fromAddr, fromKI, *idAddr, destroyCalldata, "upgrade-churn-destroy")
		if !ok {
			continue
		}
		dr := waitForMsg(n, destroyCid, "upgrade-churn-destroy")
		if dr != nil && dr.Receipt.ExitCode.IsSuccess() {
			destroyed++
		}
	}

	if deployed > 0 {
		assert.Reachable("Actor churn executed at upgrade boundary", map[string]any{
			"boundary":      b.Name,
			"height":        currentHeight,
			"upgrade_epoch": b.Epoch,
			"deployed":      deployed,
			"destroyed":     destroyed,
		})
	}

	for _, addr := range addrs[:min(3, len(addrs))] {
		verifyActorConsistency(addr, "post-upgrade-churn-"+b.Name)
	}
}
