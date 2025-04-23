package main

import (
	"context"
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
	assert.Sometimes(err == nil, "Chain head retrieval", map[string]interface{}{
		"node":     node1.Name,
		"error":    err,
		"property": "node should respond to chain head requests",
	})
	if err != nil {
		return
	}

	head2, err := api2.ChainHead(ctx)
	assert.Sometimes(err == nil, "Chain head retrieval", map[string]interface{}{
		"node":     node2.Name,
		"error":    err,
		"property": "node should respond to chain head requests",
	})
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
	assert.Sometimes(heightDiff <= 5, "Chain height synchronization", map[string]interface{}{
		"height1":  head1.Height(),
		"height2":  head2.Height(),
		"diff":     heightDiff,
		"property": "nodes should maintain similar chain heights",
		"node1":    node1.Name,
		"node2":    node2.Name,
	})

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

		assert.Sometimes(tipsetEqual, "Tipset equality", map[string]interface{}{
			"height":   commonHeight,
			"node1":    node1.Name,
			"node2":    node2.Name,
			"ts1_key":  ts1.Key().String(),
			"ts2_key":  ts2.Key().String(),
			"property": "nodes should have identical tipsets at same height",
		})
	}
}
