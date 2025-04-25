package main

import (
	"encoding/json"
	"math/rand"
	"testing"
	"time"

	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources"
	"github.com/antithesishq/antithesis-sdk-go/assert"
)

type tipSetResponse struct {
	JsonRPC string `json:"jsonrpc"`
	Result  struct {
		Height int64    `json:"Height"`
		Key    []string `json:"Key"`
	} `json:"result"`
	ID int `json:"id"`
}

// TestTipSetSelectorValidation tests that nodes return consistent tipsets for safe and finalized selectors
func TestTipSetSelectorValidation(t *testing.T) {
	// Test cases for safe tipsets
	t.Run("SafeTipSets", func(t *testing.T) {
		testSafeTipSets(t)
	})

	// Test cases for finalized tipsets
	t.Run("FinalizedTipSets", func(t *testing.T) {
		testFinalizedTipSets(t)
	})

	// Test cases for random historical tipsets
	t.Run("RandomHistoricalTipSets", func(t *testing.T) {
		testRandomHistoricalTipSets(t)
	})
}

func testSafeTipSets(t *testing.T) {
	// Create JSON-RPC request for safe tipset
	rpcBody, err := resources.CreateJSONRPCRequest("Filecoin.ChainGetTipSet", []interface{}{"safe"})
	assert.Always(err == nil, "Creating JSON-RPC request", map[string]interface{}{"error": err})

	// Send request to all nodes
	responses := resources.DoRawRequest(1, rpcBody)
	assert.Always(len(responses) > 1, "At least two nodes should respond", map[string]interface{}{
		"responses": len(responses),
	})

	// Parse first response as reference
	var refResponse tipSetResponse
	var refNodeName string
	for name, resp := range responses {
		err := json.Unmarshal(resp.Body, &refResponse)
		if err == nil {
			refNodeName = name
			break
		}
	}

	// Compare with other nodes
	for nodeName, resp := range responses {
		if nodeName == refNodeName {
			continue
		}

		var nodeResponse tipSetResponse
		err := json.Unmarshal(resp.Body, &nodeResponse)
		assert.Always(err == nil, "Parsing response from node", map[string]interface{}{
			"node":  nodeName,
			"error": err,
		})

		assert.Always(nodeResponse.Result.Height == refResponse.Result.Height,
			"Safe tipset heights should match across nodes", map[string]interface{}{
				"reference_node": refNodeName,
				"test_node":      nodeName,
				"ref_height":     refResponse.Result.Height,
				"node_height":    nodeResponse.Result.Height,
			})

		// Compare tipset keys
		assert.Always(len(nodeResponse.Result.Key) == len(refResponse.Result.Key),
			"Safe tipset key lengths should match", map[string]interface{}{
				"reference_node": refNodeName,
				"test_node":      nodeName,
			})

		for i := range refResponse.Result.Key {
			assert.Always(nodeResponse.Result.Key[i] == refResponse.Result.Key[i],
				"Safe tipset keys should match", map[string]interface{}{
					"reference_node": refNodeName,
					"test_node":      nodeName,
					"key_index":      i,
				})
		}
	}
}

func testFinalizedTipSets(t *testing.T) {
	// Create JSON-RPC request for finalized tipset
	rpcBody, err := resources.CreateJSONRPCRequest("Filecoin.ChainGetTipSet", []interface{}{"finalized"})
	assert.Always(err == nil, "Creating JSON-RPC request", map[string]interface{}{"error": err})

	// Send request to all nodes
	responses := resources.DoRawRequest(1, rpcBody)
	assert.Always(len(responses) > 1, "At least two nodes should respond", map[string]interface{}{
		"responses": len(responses),
	})

	// Parse first response as reference
	var refResponse tipSetResponse
	var refNodeName string
	for name, resp := range responses {
		err := json.Unmarshal(resp.Body, &refResponse)
		if err == nil {
			refNodeName = name
			break
		}
	}

	// Compare with other nodes
	for nodeName, resp := range responses {
		if nodeName == refNodeName {
			continue
		}

		var nodeResponse tipSetResponse
		err := json.Unmarshal(resp.Body, &nodeResponse)
		assert.Always(err == nil, "Parsing response from node", map[string]interface{}{
			"node":  nodeName,
			"error": err,
		})

		assert.Always(nodeResponse.Result.Height == refResponse.Result.Height,
			"Finalized tipset heights should match across nodes", map[string]interface{}{
				"reference_node": refNodeName,
				"test_node":      nodeName,
				"ref_height":     refResponse.Result.Height,
				"node_height":    nodeResponse.Result.Height,
			})

		// Compare tipset keys
		assert.Always(len(nodeResponse.Result.Key) == len(refResponse.Result.Key),
			"Finalized tipset key lengths should match", map[string]interface{}{
				"reference_node": refNodeName,
				"test_node":      nodeName,
			})

		for i := range refResponse.Result.Key {
			assert.Always(nodeResponse.Result.Key[i] == refResponse.Result.Key[i],
				"Finalized tipset keys should match", map[string]interface{}{
					"reference_node": refNodeName,
					"test_node":      nodeName,
					"key_index":      i,
				})
		}
	}
}

func testRandomHistoricalTipSets(t *testing.T) {
	rand.Seed(time.Now().UnixNano())

	// Get current head to determine height range
	headRPCBody, err := resources.CreateJSONRPCRequest("Filecoin.ChainHead", nil)
	assert.Always(err == nil, "Creating JSON-RPC request for chain head", map[string]interface{}{"error": err})

	responses := resources.DoRawRequest(1, headRPCBody)
	assert.Always(len(responses) > 0, "At least one node should respond", nil)

	var headResponse tipSetResponse
	for _, resp := range responses {
		err := json.Unmarshal(resp.Body, &headResponse)
		if err == nil {
			break
		}
	}

	// Test multiple random heights
	for i := 0; i < 5; i++ {
		maxHeight := headResponse.Result.Height - 900
		if maxHeight < 1 {
			continue
		}
		randomHeight := rand.Int63n(maxHeight) + 1

		// Create JSON-RPC request for historical tipset
		params := []interface{}{randomHeight, headResponse.Result.Key}
		rpcBody, err := resources.CreateJSONRPCRequest("Filecoin.ChainGetTipSetByHeight", params)
		assert.Always(err == nil, "Creating JSON-RPC request for historical tipset", map[string]interface{}{"error": err})

		// Send request to all nodes
		responses := resources.DoRawRequest(1, rpcBody)
		assert.Always(len(responses) > 1, "At least two nodes should respond", map[string]interface{}{
			"responses": len(responses),
		})

		// Parse first response as reference
		var refResponse tipSetResponse
		var refNodeName string
		for name, resp := range responses {
			err := json.Unmarshal(resp.Body, &refResponse)
			if err == nil {
				refNodeName = name
				break
			}
		}

		// Compare with other nodes
		for nodeName, resp := range responses {
			if nodeName == refNodeName {
				continue
			}

			var nodeResponse tipSetResponse
			err := json.Unmarshal(resp.Body, &nodeResponse)
			assert.Always(err == nil, "Parsing response from node", map[string]interface{}{
				"node":  nodeName,
				"error": err,
			})

			assert.Always(nodeResponse.Result.Height == refResponse.Result.Height,
				"Historical tipset heights should match across nodes", map[string]interface{}{
					"reference_node": refNodeName,
					"test_node":      nodeName,
					"height":         randomHeight,
					"ref_height":     refResponse.Result.Height,
					"node_height":    nodeResponse.Result.Height,
				})

			// Compare tipset keys
			assert.Always(len(nodeResponse.Result.Key) == len(refResponse.Result.Key),
				"Historical tipset key lengths should match", map[string]interface{}{
					"reference_node": refNodeName,
					"test_node":      nodeName,
					"height":         randomHeight,
				})

			for i := range refResponse.Result.Key {
				assert.Always(nodeResponse.Result.Key[i] == refResponse.Result.Key[i],
					"Historical tipset keys should match", map[string]interface{}{
						"reference_node": refNodeName,
						"test_node":      nodeName,
						"height":         randomHeight,
						"key_index":      i,
					})
			}
		}
	}
}
