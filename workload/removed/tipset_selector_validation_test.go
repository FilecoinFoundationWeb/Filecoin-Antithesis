package main

import (
	"context"
	"fmt"
	"log"
	"testing"

	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources"
	"github.com/antithesishq/antithesis-sdk-go/assert"
)

func TestTipsetConsistencySelector(t *testing.T) {
	ctx := context.Background()
	log.Printf("Starting tipset consistency test...")

	// Get the current tipset height from finalized tipset
	var currentHeight int

	// Test finalized tipsets (should always be consistent)
	log.Printf("Checking finalized tipset consistency...")
	finalizedResponses, err := resources.GetFinalizedTipSet()
	if err != nil {
		log.Fatalf("Failed to get finalized tipsets: %v", err)
	}

	match, cidsByNode, heightsByNode, err := resources.CompareTipSetResponses(finalizedResponses)
	if err != nil {
		log.Fatalf("Failed to compare finalized tipsets: %v", err)
	}

	log.Printf("Finalized tipset consistency check: %v", match)
	for node, cids := range cidsByNode {
		log.Printf("Node %s: height=%d, CIDs=%v", node, heightsByNode[node], cids)
		currentHeight = heightsByNode[node]
		break
	}

	assert.Always(match, "Finalized tipsets must be consistent across nodes",
		map[string]interface{}{"cidsByNode": cidsByNode, "heightsByNode": heightsByNode})

	// Test finalized tipsets with explicit height parameter
	log.Printf("Checking finalized tipset with height %d...", currentHeight)
	responses, err := resources.GetTipSetBySelectorAndHeight("finalized", int64(currentHeight))
	if err != nil {
		log.Fatalf("Failed to get finalized tipset at height %d: %v", currentHeight, err)
	}

	match, cidsByNode, heightsByNode, err = resources.CompareTipSetResponses(responses)
	if err != nil {
		log.Fatalf("Failed to compare finalized tipsets at height %d: %v", currentHeight, err)
	}

	log.Printf("Finalized tipset at height %d consistency check: %v", currentHeight, match)
	for node, cids := range cidsByNode {
		log.Printf("Node %s: height=%d, CIDs=%v", node, heightsByNode[node], cids)
	}

	assert.Always(match, fmt.Sprintf("Finalized tipsets at height %d must be consistent across nodes", currentHeight),
		map[string]interface{}{"cidsByNode": cidsByNode, "heightsByNode": heightsByNode})

	// Test with other selectors and height
	testSelectorWithHeight(ctx, "latest", currentHeight)
	testSelectorWithHeight(ctx, "safe", currentHeight)

	// Also test without height parameter (regular behavior)
	checkTipSetType(ctx, "latest")
	checkTipSetType(ctx, "safe")
}

func checkTipSetType(ctx context.Context, selector string) {
	log.Printf("Checking %s tipset...", selector)

	responses, err := resources.GetTipSetBySelector(selector)
	if err != nil {
		log.Fatalf("Failed to get %s tipsets: %v", selector, err)
	}

	match, cidsByNode, heightsByNode, err := resources.CompareTipSetResponses(responses)
	if err != nil {
		log.Fatalf("Failed to compare %s tipsets: %v", selector, err)
	}

	log.Printf("%s tipset consistency check: %v", selector, match)
	for node, cids := range cidsByNode {
		log.Printf("Node %s: height=%d, CIDs=%v", node, heightsByNode[node], cids)
	}

	assert.Sometimes(match, fmt.Sprintf("%s tipsets may not be consistent across nodes", selector),
		map[string]interface{}{"cidsByNode": cidsByNode, "heightsByNode": heightsByNode})
}

func testSelectorWithHeight(ctx context.Context, selector string, height int) {
	log.Printf("Checking %s tipset with height %d...", selector, height)

	responses, err := resources.GetTipSetBySelectorAndHeight(selector, int64(height))
	if err != nil {
		log.Fatalf("Failed to get %s tipset at height %d: %v", selector, height, err)
	}

	match, cidsByNode, heightsByNode, err := resources.CompareTipSetResponses(responses)
	if err != nil {
		log.Fatalf("Failed to compare %s tipsets at height %d: %v", selector, height, err)
	}

	log.Printf("%s tipset at height %d consistency check: %v", selector, height, match)
	for node, cids := range cidsByNode {
		log.Printf("Node %s: height=%d, CIDs=%v", node, heightsByNode[node], cids)
	}

	// Tipsets with a specific selector and height should be consistent
	assert.Always(match, fmt.Sprintf("%s tipsets at height %d must be consistent across nodes", selector, height),
		map[string]interface{}{"cidsByNode": cidsByNode, "heightsByNode": heightsByNode})
}
