package main

import (
	"log"
	"sync"

	"github.com/antithesishq/antithesis-sdk-go/assert"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/network"
)

// ===========================================================================
// Generic Network Upgrade Test Suite
//
// Upgrade-agnostic checks that run for every configured boundary:
//   1. Every node's network version advanced across the boundary.
//   2. Every node reports the same basefee at a finalized post-boundary height.
//
// FIP-specific tests (e.g. FIP-0115 in NV28) live in per-upgrade files
// (nv28_vectors.go) and register themselves via RegisterUpgradeVector.
//
// Self-gates per boundary: active in [epoch-10, epoch+30]; silent otherwise.
// ===========================================================================

const upgradeBoundaryWindow = 30

type upgradeBoundary struct {
	Name  string
	Epoch abi.ChainEpoch
}

var (
	upgradeOnce       sync.Once
	upgradeBoundaries []upgradeBoundary
)

// UpgradeVectorFunc is a FIP-specific test run per boundary. It must
// self-gate on b.Name (e.g. "NV28") and on currentHeight.
type UpgradeVectorFunc func(currentHeight abi.ChainEpoch, b upgradeBoundary)

var upgradeVectors []UpgradeVectorFunc

// RegisterUpgradeVector adds a FIP-specific test to the upgrade suite.
// Call from init() in per-upgrade files (e.g. nv28_vectors.go).
func RegisterUpgradeVector(fn UpgradeVectorFunc) {
	upgradeVectors = append(upgradeVectors, fn)
}

func initUpgradeState() {
	upgradeOnce.Do(func() {
		if g := abi.ChainEpoch(envInt("GOLDENWEEK_HEIGHT", 0)); g > 0 {
			upgradeBoundaries = append(upgradeBoundaries, upgradeBoundary{"NV27", g})
		}
		if x := abi.ChainEpoch(envInt("FIREHORSE_HEIGHT", 0)); x > 0 {
			upgradeBoundaries = append(upgradeBoundaries, upgradeBoundary{"NV28", x})
		}
	})
}

func nearUpgrade(height abi.ChainEpoch, b upgradeBoundary) bool {
	return height >= b.Epoch-10 && height <= b.Epoch+abi.ChainEpoch(upgradeBoundaryWindow)
}

func DoUpgradeSuite() {
	initUpgradeState()
	if len(upgradeBoundaries) == 0 {
		return
	}

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

	for _, b := range upgradeBoundaries {
		if !nearUpgrade(currentHeight, b) {
			continue
		}
		doUpgradeActivation(currentHeight, b)
		doBaseFeeAgreement(b)
		for _, fn := range upgradeVectors {
			fn(currentHeight, b)
		}
	}
}

// doUpgradeActivation — every node's NV must advance across the boundary.
func doUpgradeActivation(currentHeight abi.ChainEpoch, b upgradeBoundary) {
	if currentHeight <= b.Epoch+5 || b.Epoch < 2 {
		return
	}

	perNode := make(map[string]map[string]network.Version)
	allActivated := true
	checked := 0

	for _, name := range nodeKeys {
		n := nodes[name]
		head, err := n.ChainHead(ctx)
		if err != nil {
			continue
		}

		postTs, err := n.ChainGetTipSetByHeight(ctx, b.Epoch+1, head.Key())
		if err != nil {
			continue
		}
		// Null-round guard: if epoch+1 resolved below the boundary, skip.
		if postTs.Height() < b.Epoch {
			continue
		}
		postNV, err := n.StateNetworkVersion(ctx, postTs.Key())
		if err != nil {
			continue
		}

		preTs, err := n.ChainGetTipSetByHeight(ctx, b.Epoch-1, head.Key())
		if err != nil {
			continue
		}
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
		"boundary":      b.Name,
		"upgrade_epoch": b.Epoch,
		"per_node":      perNode,
		"checked":       checked,
	})

	if !allActivated {
		log.Printf("[upgrade/%s] ACTIVATION DIVERGENCE at epoch %d: %v", b.Name, b.Epoch, perNode)
	}
}

// doBaseFeeAgreement — basefee must agree across all nodes at a finalized
// post-boundary height. Uses the shared finalized anchor so every node is
// queried against the same tipset key.
func doBaseFeeAgreement(b upgradeBoundary) {
	if len(nodeKeys) < 2 {
		return
	}

	snap := getFinalizedSnapshots()
	finalizedHeight, anchorKey := snapshotMinHeight(snap)
	if finalizedHeight < b.Epoch+2 {
		return
	}

	checkHeight := b.Epoch + 1

	baseFees := make(map[string][]string)
	for name, s := range snap {
		if s.err != nil {
			continue
		}
		ts, err := nodes[name].ChainGetTipSetByHeight(ctx, checkHeight, anchorKey)
		if err != nil || len(ts.Blocks()) == 0 {
			continue
		}
		fee := ts.Blocks()[0].ParentBaseFee.String()
		baseFees[fee] = append(baseFees[fee], name)
	}

	responded := 0
	for _, names := range baseFees {
		responded += len(names)
	}
	if responded < 2 {
		return
	}

	agreed := len(baseFees) == 1

	assert.Always(agreed, "Basefee agrees across nodes post-upgrade", map[string]any{
		"boundary":      b.Name,
		"height":        checkHeight,
		"upgrade_epoch": b.Epoch,
		"base_fees":     baseFees,
		"responded":     responded,
	})

	if !agreed {
		log.Printf("[upgrade/%s] BASEFEE DIVERGENCE at post-upgrade epoch %d: %v", b.Name, checkHeight, baseFees)
	}
}
