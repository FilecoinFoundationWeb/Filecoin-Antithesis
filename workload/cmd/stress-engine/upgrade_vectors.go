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
// version transition. Two categories:
//
//   GENERIC — run for every upgrade, no FIP knowledge needed:
//     - Network version agreement across nodes
//     - State root agreement at migration epoch
//     - Receipt consistency at boundary
//     - Upgrade activation liveness
//
//   FIP-SPECIFIC — registered by per-upgrade files (e.g. nv28_vectors.go):
//     - Custom boundary stress functions
//     - Precompile/opcode tests
//     - Gas formula validation
//
// The suite self-gates: active around the upgrade boundary window, becomes
// a no-op once the chain is 30+ epochs past the upgrade.
// ===========================================================================

// ---------------------------------------------------------------------------
// Upgrade state tracking
// ---------------------------------------------------------------------------

const upgradeBoundaryWindow = 30 // epochs past upgrade before suite goes quiet

var (
	upgradeOnce     sync.Once
	upgradeEpoch    abi.ChainEpoch
	preUpgradeNV    network.Version
	upgradeDetected bool
)

func initUpgradeState() {
	upgradeOnce.Do(func() {
		upgradeEpoch = abi.ChainEpoch(envInt("XX_HEIGHT", 99999))
	})
}

// extractActorID decodes a CreateExternalReturn from a message lookup result.
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

// nearUpgrade returns true if height is within the active boundary window.
func nearUpgrade(height abi.ChainEpoch) bool {
	return height >= upgradeEpoch-10 && height <= upgradeEpoch+abi.ChainEpoch(upgradeBoundaryWindow)
}

// ---------------------------------------------------------------------------
// FIP-specific hook: per-upgrade files register boundary stress functions
// ---------------------------------------------------------------------------

// UpgradeBoundaryFunc is a function that runs FIP-specific stress at the
// upgrade boundary. It receives the current chain height so it can self-gate.
type UpgradeBoundaryFunc func(currentHeight abi.ChainEpoch)

var fipBoundaryFuncs []UpgradeBoundaryFunc

// RegisterFIPBoundaryFunc adds a FIP-specific stress function to the suite.
// Call from init() in per-upgrade files (e.g. nv28_vectors.go).
func RegisterFIPBoundaryFunc(fn UpgradeBoundaryFunc) {
	fipBoundaryFuncs = append(fipBoundaryFuncs, fn)
}

// ---------------------------------------------------------------------------
// DoUpgradeSuite — single deck entry
//
// Active from upgradeEpoch-10 through upgradeEpoch+30. Before and after
// that window, returns immediately (no deck time wasted).
//
// Runs generic assertions first, then any registered FIP-specific functions.
// ---------------------------------------------------------------------------

func DoUpgradeSuite() {
	initUpgradeState()

	// Use max chain head across all nodes — a crashed/restarted node shouldn't
	// suppress the suite for everyone else.
	var currentHeight abi.ChainEpoch
	for _, name := range nodeKeys {
		head, err := nodes[name].ChainHead(ctx)
		if err != nil {
			continue
		}
		if head.Height() > currentHeight {
			currentHeight = head.Height()
		}
	}
	if currentHeight == 0 {
		return
	}

	// Self-gate: only active around the upgrade boundary
	if currentHeight < upgradeEpoch-10 || currentHeight > upgradeEpoch+abi.ChainEpoch(upgradeBoundaryWindow) {
		return
	}

	// --- Generic assertions (any upgrade) ---
	doNetworkVersionAgreement()
	doUpgradeActivation(currentHeight)
	doMigrationStateRootAgreement()
	doReceiptConsistencyAtBoundary()

	// --- Generic boundary stress ---
	doBoundaryMessageBurst(currentHeight)
	doActorChurnAtBoundary(currentHeight)

	// --- FIP-specific hooks ---
	for _, fn := range fipBoundaryFuncs {
		fn(currentHeight)
	}
}

// ===========================================================================
// Generic Assertions
// ===========================================================================

// doNetworkVersionAgreement — all nodes must report the same NV at finalized height.
func doNetworkVersionAgreement() {
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
		"height":       checkHeight,
		"finalized_at": finalizedHeight,
		"versions":     versions,
		"responded":    responded,
		"near_upgrade": nearUpgrade(checkHeight),
	})

	if !agreed {
		log.Printf("[upgrade] NETWORK VERSION DIVERGENCE at height %d: %v", checkHeight, versions)
	}

	// Track NV for activation check
	if checkHeight < upgradeEpoch && preUpgradeNV == 0 {
		for nv := range versions {
			preUpgradeNV = nv
		}
	}
	if !upgradeDetected && checkHeight > upgradeEpoch && preUpgradeNV > 0 {
		for nv := range versions {
			if nv > preUpgradeNV {
				upgradeDetected = true
				log.Printf("[upgrade] detected: NV %d → %d at epoch %d", preUpgradeNV, nv, upgradeEpoch)
			}
		}
	}
}

// doUpgradeActivation — liveness: NV must advance after upgrade epoch.
func doUpgradeActivation(currentHeight abi.ChainEpoch) {
	if currentHeight <= upgradeEpoch+5 || upgradeEpoch < 2 {
		return
	}

	node := nodes[nodeKeys[0]]
	head, err := node.ChainHead(ctx)
	if err != nil {
		return
	}

	postTs, err := node.ChainGetTipSetByHeight(ctx, upgradeEpoch+1, head.Key())
	if err != nil {
		return
	}
	postNV, err := node.StateNetworkVersion(ctx, postTs.Key())
	if err != nil {
		return
	}

	preTs, err := node.ChainGetTipSetByHeight(ctx, upgradeEpoch-1, head.Key())
	if err != nil {
		return
	}
	preNV, err := node.StateNetworkVersion(ctx, preTs.Key())
	if err != nil {
		return
	}

	assert.Sometimes(postNV > preNV, "Network upgrade activated at configured epoch", map[string]any{
		"upgrade_epoch": upgradeEpoch,
		"pre_nv":        preNV,
		"post_nv":       postNV,
	})
}

// doMigrationStateRootAgreement — state roots at epoch-1, epoch, epoch+1 must match.
func doMigrationStateRootAgreement() {
	if len(nodeKeys) < 2 {
		return
	}

	snap := getFinalizedSnapshots()
	finalizedHeight, anchorKey := snapshotMinHeight(snap)
	if finalizedHeight < upgradeEpoch+2 {
		return
	}

	for _, offset := range []abi.ChainEpoch{-1, 0, 1} {
		checkHeight := upgradeEpoch + offset
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
			"height":        checkHeight,
			"upgrade_epoch": upgradeEpoch,
			"phase":         phase,
			"state_roots":   stateRoots,
			"responded":     totalResponded,
		})

		if !agreed {
			log.Printf("[upgrade] STATE ROOT DIVERGENCE %s migration (epoch %d): %v", phase, checkHeight, stateRoots)
		}
	}
}

// doReceiptConsistencyAtBoundary — receipt roots at upgrade+1 must match.
func doReceiptConsistencyAtBoundary() {
	if len(nodeKeys) < 2 {
		return
	}

	snap := getFinalizedSnapshots()
	finalizedHeight, anchorKey := snapshotMinHeight(snap)
	if finalizedHeight < upgradeEpoch+2 {
		return
	}

	checkHeight := upgradeEpoch + 1

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
		"height":        checkHeight,
		"upgrade_epoch": upgradeEpoch,
		"receipt_roots": receiptRoots,
		"responded":     totalResponded,
	})

	if !agreed {
		log.Printf("[upgrade] RECEIPT DIVERGENCE at post-upgrade epoch %d: %v", checkHeight, receiptRoots)
	}
}

// ===========================================================================
// Generic Boundary Stress
// ===========================================================================

// doBoundaryMessageBurst — submits messages right before upgrade so they're
// in-flight when the state migration runs.
func doBoundaryMessageBurst(currentHeight abi.ChainEpoch) {
	epochsUntilUpgrade := upgradeEpoch - currentHeight
	if epochsUntilUpgrade < 0 || epochsUntilUpgrade > 5 {
		return
	}

	log.Printf("[upgrade-stress] boundary burst: %d epochs until upgrade", epochsUntilUpgrade)

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
			debugLog("[upgrade-stress] burst msg %d via %s", i, nodeName)
		}
	}

	if sent > 0 {
		assert.Reachable("Boundary message burst submitted before upgrade", map[string]any{
			"sent":           sent,
			"epochs_until":   epochsUntilUpgrade,
			"upgrade_epoch":  upgradeEpoch,
		})
	}

	// Verify state after burst lands
	time.Sleep(15 * time.Second)
	for _, addr := range addrs[:min(5, len(addrs))] {
		verifyActorConsistency(addr, "post-upgrade-burst")
	}
}

// doActorChurnAtBoundary — burst deploy/destroy around the upgrade epoch
// to stress HAMT during migration.
func doActorChurnAtBoundary(currentHeight abi.ChainEpoch) {
	distance := currentHeight - upgradeEpoch
	if distance < 0 {
		distance = -distance
	}
	if distance > 5 {
		return
	}

	log.Printf("[upgrade-stress] actor churn: height=%d upgrade=%d", currentHeight, upgradeEpoch)

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
			"height":       currentHeight,
			"upgrade_epoch": upgradeEpoch,
			"deployed":     deployed,
			"destroyed":    destroyed,
		})
	}

	for _, addr := range addrs[:min(3, len(addrs))] {
		verifyActorConsistency(addr, "post-upgrade-churn")
	}
}
