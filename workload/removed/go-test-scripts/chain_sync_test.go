package main

import (
	"context"
	"fmt"
	"testing"

	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources"
	"github.com/antithesishq/antithesis-sdk-go/assert"
)

func TestChainSync(t *testing.T) {
	ctx := context.Background()

	config, err := resources.LoadConfig("/opt/antithesis/resources/config.json")
	if err != nil {
		t.Skip("Skipping test: failed to load config")
	}

	if len(config.Nodes) < 2 {
		t.Skip("Skipping test: need at least two nodes")
	}

	node1 := config.Nodes[0]
	node2 := config.Nodes[1]

	api1, closer1, err := resources.ConnectToNode(ctx, node1)
	if err != nil {
		t.Skip("Skipping test: could not connect to first node")
	}
	defer closer1()

	api2, closer2, err := resources.ConnectToNode(ctx, node2)
	if err != nil {
		t.Skip("Skipping test: could not connect to second node")
	}
	defer closer2()

	head1, err := api1.ChainHead(ctx)
	assert.Sometimes(err == nil,
		fmt.Sprintf("[Chain Head Check] Node %s should successfully retrieve its chain head. This verifies the node's ability to access its blockchain state.", node1.Name),
		resources.EnhanceAssertDetails(
			map[string]interface{}{
				"error":    err,
				"property": "Chain head retrieval capability",
				"impact":   "Critical for node synchronization and state access",
				"details":  "A node must be able to access its current chain head to participate in the network",
			},
			node1.Name,
		))
	if err != nil {
		return
	}

	head2, err := api2.ChainHead(ctx)
	assert.Sometimes(err == nil,
		fmt.Sprintf("[Chain Head Check] Node %s should successfully retrieve its chain head. This verifies the node's ability to access its blockchain state.", node2.Name),
		resources.EnhanceAssertDetails(
			map[string]interface{}{
				"error":    err,
				"property": "Chain head retrieval capability",
				"impact":   "Critical for node synchronization and state access",
				"details":  "A node must be able to access its current chain head to participate in the network",
			},
			node2.Name,
		))
	if err != nil {
		return
	}

	if head1 == nil || head2 == nil {
		t.Skip("Skipping test: chain heads are nil")
	}

	t.Logf("Chain heights: %s=%d, %s=%d", node1.Name, head1.Height(), node2.Name, head2.Height())

	// Check height difference
	heightDiff := head1.Height() - head2.Height()
	if heightDiff < 0 {
		heightDiff = -heightDiff // Absolute value
	}

	// Simple check for reasonable height difference
	assert.Sometimes(heightDiff <= 5,
		"[Chain Height Sync] Nodes should maintain similar chain heights within an acceptable difference (≤ 5 blocks). Large differences may indicate network partitioning or sync issues.",
		resources.EnhanceAssertDetails(
			map[string]interface{}{
				"height1":        head1.Height(),
				"height2":        head2.Height(),
				"diff":           heightDiff,
				"max_allowed":    5,
				"property":       "Chain height synchronization",
				"impact":         "Critical for network consensus",
				"details":        "Height differences > 5 blocks may indicate network issues or node isolation",
				"recommendation": "If failing, check network connectivity and node health",
				"node1":          node1.Name,
				"node2":          node2.Name,
			},
			"multiple_nodes",
		))

	// Check just one tipset to confirm basic synchronization
	commonHeight := head1.Height()
	if head2.Height() < commonHeight {
		commonHeight = head2.Height()
	}

	// Go back 1 block to ensure both nodes have this tipset
	if commonHeight > 0 {
		commonHeight--
	}

	ts1, err := api1.ChainGetTipSetByHeight(ctx, commonHeight, head1.Key())
	ts2, err2 := api2.ChainGetTipSetByHeight(ctx, commonHeight, head2.Key())

	if err == nil && err2 == nil && ts1 != nil && ts2 != nil {
		tipsetEqual := ts1.Equals(ts2)
		t.Logf("Tipsets at height %d equal: %v", commonHeight, tipsetEqual)

		assert.Sometimes(tipsetEqual,
			"[Tipset Consistency] Nodes must have identical tipsets at the same height. Different tipsets indicate consensus failure or chain fork.",
			resources.EnhanceAssertDetails(
				map[string]interface{}{
					"height":         commonHeight,
					"ts1_key":        ts1.Key().String(),
					"ts2_key":        ts2.Key().String(),
					"property":       "Tipset consistency across nodes",
					"impact":         "Critical for network consensus",
					"details":        "Different tipsets at same height indicate potential chain fork",
					"recommendation": "If failing, investigate recent network events and node logs",
				},
				"multiple_nodes",
			))
	}
}
