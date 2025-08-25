package resources

import (
	"context"
	"encoding/json"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/antithesishq/antithesis-sdk-go/assert"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/types"
)

type ConsensusChecker struct {
	nodes map[string]NodeInfo
}

type NodeInfo struct {
	Name   string
	RPCURL string
}

type RPCResponse struct {
	Jsonrpc string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result"`
	ID      int             `json:"id"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

const (
	// Maximum number of retries for consensus checks
	maxConsensusRetries = 3
	// Time to wait between retries
	retryDelay = 5 * time.Second
	// Time to wait for tipsets to settle after getting chain head
	settlingPeriod = 10 * time.Second
)

// NewConsensusChecker creates a new consensus checker instance for Lotus nodes
// It initializes connections to both V1 and V2 API endpoints
func NewConsensusChecker(ctx context.Context, nodes []NodeConfig) (*ConsensusChecker, error) {
	checker := &ConsensusChecker{
		nodes: make(map[string]NodeInfo),
	}

	// Only handle Lotus nodes
	checker.nodes["Lotus1"] = NodeInfo{
		Name:   "Lotus1",
		RPCURL: "http://lotus-1:1234/rpc/v1",
	}
	checker.nodes["Lotus2"] = NodeInfo{
		Name:   "Lotus2",
		RPCURL: "http://lotus-2:1235/rpc/v1",
	}
	checker.nodes["Lotus1-V2"] = NodeInfo{
		Name:   "Lotus1-V2",
		RPCURL: "http://lotus-1:1234/rpc/v2",
	}
	checker.nodes["Lotus2-V2"] = NodeInfo{
		Name:   "Lotus2-V2",
		RPCURL: "http://lotus-2:1235/rpc/v2",
	}

	for name, node := range checker.nodes {
		log.Printf("Added node to consensus checker: %s with URL: %s", name, node.RPCURL)
	}

	if len(checker.nodes) < 2 {
		log.Printf("[ERROR] Need at least 2 Lotus nodes for consensus checking")
		return nil, nil
	}

	return checker, nil
}

// makeRPCRequest performs a JSON-RPC request to a node with the specified method and parameters
func (cc *ConsensusChecker) makeRPCRequest(ctx context.Context, nodeInfo NodeInfo, method string, params interface{}) (*RPCResponse, error) {
	reqBody, err := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
		"id":      1,
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", nodeInfo.RPCURL, strings.NewReader(string(reqBody)))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var rpcResp RPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return nil, err
	}

	if rpcResp.Error != nil {
		log.Printf("[ERROR] RPC error: %s", rpcResp.Error.Message)
		return nil, nil
	}

	return &rpcResp, nil
}

// getChainHead gets the current chain head for a node
// It handles both V1 and V2 API versions
func (cc *ConsensusChecker) getChainHead(ctx context.Context, nodeInfo NodeInfo) (abi.ChainEpoch, error) {
	resp, err := cc.makeRPCRequest(ctx, nodeInfo, "Filecoin.ChainHead", []interface{}{})
	if err != nil {
		return 0, err
	}

	var head struct {
		Height abi.ChainEpoch `json:"Height"`
	}
	if err := json.Unmarshal(resp.Result, &head); err != nil {
		return 0, err
	}
	return head.Height, nil
}

// getTipsetAtHeight gets the tipset at a specific height for a node
// For V2 nodes, it uses the finalized tipset selector
func (cc *ConsensusChecker) getTipsetAtHeight(ctx context.Context, nodeInfo NodeInfo, height abi.ChainEpoch) (string, error) {
	// For V2 nodes, use the finalized tipset selector
	if strings.Contains(nodeInfo.RPCURL, "/rpc/v2") {
		selector := types.TipSetSelectors.Height(height, true, types.TipSetAnchors.Finalized)
		resp, err := cc.makeRPCRequest(ctx, nodeInfo, "Filecoin.ChainGetTipSet", []interface{}{selector})
		if err != nil {
			return "", err
		}
		var tipset types.TipSet
		if err := json.Unmarshal(resp.Result, &tipset); err != nil {
			return "", err
		}
		return tipset.Key().String(), nil
	}

	// For V1 nodes, use the old method
	resp, err := cc.makeRPCRequest(ctx, nodeInfo, "Filecoin.ChainGetTipSetByHeight", []interface{}{height, types.EmptyTSK})
	if err != nil {
		return "", err
	}

	var tipset types.TipSet
	if err := json.Unmarshal(resp.Result, &tipset); err != nil {
		return "", err
	}
	return tipset.Key().String(), nil
}

// checkTipsetConsensus checks if all nodes agree on the tipset at a given height
// Returns true if consensus is reached, false otherwise
func (cc *ConsensusChecker) checkTipsetConsensus(ctx context.Context, height abi.ChainEpoch) (bool, map[string][]string, error) {
	var wg sync.WaitGroup
	type nodeResult struct {
		name      string
		tipsetKey string
		err       error
	}

	results := make(chan nodeResult, len(cc.nodes))

	for nodeName, nodeInfo := range cc.nodes {
		wg.Add(1)
		go func(name string, info NodeInfo) {
			defer wg.Done()
			result := nodeResult{name: name}

			tipsetKey, err := cc.getTipsetAtHeight(ctx, info, height)
			if err != nil {
				log.Printf("[ERROR] Failed to get tipset at height %d from %s: %v", height, name, err)
				result.err = err
			} else {
				result.tipsetKey = tipsetKey
			}
			results <- result
		}(nodeName, nodeInfo)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	tipsetKeys := make(map[string][]string)
	var errors []error

	for result := range results {
		if result.err != nil {
			errors = append(errors, result.err)
			continue
		}
		tipsetKeys[result.tipsetKey] = append(tipsetKeys[result.tipsetKey], result.name)
	}

	if len(errors) > 0 {
		log.Printf("[ERROR] Errors during consensus check at height %d: %v", height, errors)
		return false, tipsetKeys, nil
	}

	return len(tipsetKeys) == 1, tipsetKeys, nil
}

// CheckConsensus verifies that all nodes have consensus on tipsets for a range of heights
// If no height is specified (height = 0), it chooses a random height between genesis and minHead-20
func (cc *ConsensusChecker) CheckConsensus(ctx context.Context, height abi.ChainEpoch) error {
	return RetryOperation(ctx, func() error {
		log.Printf("Starting consensus check between nodes")

		// Validate height is not negative
		if height < 0 {
			log.Printf("Invalid height %d provided, will use random height instead", height)
			height = 0
		}

		// First, get chain heads to determine height range
		var minHead abi.ChainEpoch
		first := true
		for nodeName, nodeInfo := range cc.nodes {
			head, err := cc.getChainHead(ctx, nodeInfo)
			if err != nil {
				log.Printf("[ERROR] Failed to get chain head from %s: %v", nodeName, err)
				return nil
			}
			if first || head < minHead {
				minHead = head
				first = false
			}
		}

		// Choose a random height between genesis (1) and minHead-20 (to avoid reorgs)
		if height == 0 {
			if minHead < 30 {
				log.Printf("[ERROR] Chain too short for consensus check, height: %d", minHead)
				return nil
			}
			safeMaxHeight := minHead - 20 // Stay away from head to avoid reorgs
			height = abi.ChainEpoch(rand.Int63n(int64(safeMaxHeight-1))) + 1
		} else if height > minHead {
			log.Printf("Requested height %d is beyond current chain head %d, will use random height instead", height, minHead)
			height = 0
			if minHead < 30 {
				log.Printf("[ERROR] Chain too short for consensus check, height: %d", minHead)
				return nil
			}
			safeMaxHeight := minHead - 20
			height = abi.ChainEpoch(rand.Int63n(int64(safeMaxHeight-1))) + 1
		}

		// Ensure we have enough height to walk back without going negative
		if height < 10 {
			log.Printf("Height %d is too low for 10-block consensus walk, adjusting starting height", height)
			height = 10
		}

		log.Printf("Starting consensus walk from height %d", height)

		// Wait for settling period after getting chain head
		log.Printf("Waiting %s for tipsets to settle...", settlingPeriod)
		time.Sleep(settlingPeriod)

		// Check consensus for 10 consecutive tipsets
		for i := 0; i < 10; i++ {
			currentHeight := height - abi.ChainEpoch(i)
			log.Printf("Checking consensus at height %d", currentHeight)

			// Try multiple times with delay between attempts
			var consensusReached bool
			var tipsetKeys map[string][]string
			var lastErr error

			for retry := 0; retry < maxConsensusRetries; retry++ {
				if retry > 0 {
					log.Printf("Retry %d/%d for height %d after %s delay", retry+1, maxConsensusRetries, currentHeight, retryDelay)
					time.Sleep(retryDelay)
				}

				consensusReached, tipsetKeys, lastErr = cc.checkTipsetConsensus(ctx, currentHeight)
				if lastErr != nil {
					log.Printf("Error checking consensus (attempt %d/%d): %v", retry+1, maxConsensusRetries, lastErr)
					continue
				}

				if consensusReached {
					log.Printf("Consensus reached at height %d on attempt %d", currentHeight, retry+1)
					break
				}

				log.Printf("Consensus not reached at height %d on attempt %d, tipset distribution: %v",
					currentHeight, retry+1, tipsetKeys)
			}

			// After all retries, make the final assertion
			assert.Always(consensusReached,
				"[Consensus Check] All nodes must agree on the same tipset",
				map[string]interface{}{
					"height":         currentHeight,
					"tipset_keys":    tipsetKeys,
					"property":       "Chain consensus",
					"impact":         "Critical - indicates chain fork or consensus failure",
					"details":        "All nodes must have identical tipsets at each height",
					"recommendation": "Check network connectivity and sync status",
					"nodes_checked":  len(cc.nodes),
					"unique_tipsets": len(tipsetKeys),
					"retries":        maxConsensusRetries,
				})

			log.Printf("Consensus verified at height %d", currentHeight)
		}

		log.Printf("Consensus walk completed successfully from height %d to %d", height, height-9)
		return nil
	}, "Consensus check operation")
}

// PerformConsensusCheck checks consensus between nodes
func PerformConsensusCheck(ctx context.Context, config *Config, height int64) error {
	log.Printf("[INFO] Starting consensus check...")

	checker, err := NewConsensusChecker(ctx, config.Nodes)
	if err != nil {
		log.Printf("[ERROR] Failed to create consensus checker: %v", err)
		return nil
	}

	// If height is 0, we'll let the checker pick a random height
	if height == 0 {
		log.Printf("[INFO] No specific height provided, will check consensus at a random height")
	} else {
		log.Printf("[INFO] Will check consensus starting at height %d", height)
	}

	// Run the consensus check
	err = checker.CheckConsensus(ctx, abi.ChainEpoch(height))
	if err != nil {
		log.Printf("[WARN] Consensus check failed, will retry: %v", err)
		return err // Return original error for retry
	}

	log.Printf("[INFO] Consensus check completed successfully")
	return nil
}

// PerformSendConsensusFault sends a consensus fault
func PerformSendConsensusFault(ctx context.Context) error {
	log.Println("[INFO] Attempting to send a consensus fault...")
	err := SendConsensusFault(ctx)
	if err != nil {
		log.Printf("[ERROR] Failed to send consensus fault: %v", err)
		return nil
	}
	log.Println("[INFO] SendConsensusFault operation initiated. Check further logs for details.")
	return nil
}

// PerformCheckFinalizedTipsets checks finalized tipsets
func PerformCheckFinalizedTipsets(ctx context.Context) error {
	log.Printf("[INFO] Starting finalized tipset comparison...")

	// Load configuration
	config, err := LoadConfig("/opt/antithesis/resources/config.json")
	if err != nil {
		log.Printf("[ERROR] Failed to load config: %v", err)
		return nil
	}

	// Filter nodes to get V1 and V2 nodes separately
	v1Nodes := FilterLotusNodesV1(config.Nodes)
	v2Nodes := FilterLotusNodesWithV2(config.Nodes)

	if len(v1Nodes) < 2 {
		log.Printf("[ERROR] Need at least two Lotus V1 nodes for this test, found %d", len(v1Nodes))
		return nil
	}
	if len(v2Nodes) < 2 {
		log.Printf("[ERROR] Need at least two Lotus V2 nodes for this test, found %d", len(v2Nodes))
		return nil
	}

	// Connect to V1 nodes to get chain heads and find common height range
	api1, closer1, err := ConnectToNode(ctx, v1Nodes[0])
	if err != nil {
		log.Printf("[ERROR] Failed to connect to %s: %v", v1Nodes[0].Name, err)
		return nil
	}
	defer closer1()

	api2, closer2, err := ConnectToNode(ctx, v1Nodes[1])
	if err != nil {
		log.Printf("[ERROR] Failed to connect to %s: %v", v1Nodes[1].Name, err)
		return nil
	}
	defer closer2()

	ch1, err := api1.ChainHead(ctx)
	if err != nil {
		log.Printf("[ERROR] Failed to get chain head from %s: %v", v1Nodes[0].Name, err)
		return nil
	}

	ch2, err := api2.ChainHead(ctx)
	if err != nil {
		log.Printf("[ERROR] Failed to get chain head from %s: %v", v1Nodes[1].Name, err)
		return nil
	}

	h1 := ch1.Height()
	h2 := ch2.Height()

	log.Printf("[INFO] Current height %d for node %s", h1, v1Nodes[0].Name)
	log.Printf("[INFO] Current height %d for node %s", h2, v1Nodes[1].Name)

	// Find the common height between both nodes
	var commonHeight int64
	if h1 < h2 {
		commonHeight = int64(h1)
	} else {
		commonHeight = int64(h2)
	}

	// Ensure we have enough history for F3 finalized tipset comparison
	// F3 starts from epoch 20, so we need at least 30 epochs to have a meaningful range
	if commonHeight < 30 {
		log.Printf("[WARN] chain height too low for finalized tipset comparison (common: %d, required: 30 for F3 range)", commonHeight)
		return nil
	}

	// Select a random height within the F3 range
	// F3 starts from epoch 20, and we leave 10 epochs buffer from the tip
	rand.Seed(time.Now().UnixNano())
	maxHeight := commonHeight - 10 // Leave 10 epochs buffer from tip
	minHeight := int64(20)         // F3 starts from epoch 20

	if maxHeight <= minHeight {
		log.Printf("[WARN] Not enough height range for finalized tipset comparison (min: %d, max: %d)", minHeight, maxHeight)
		return nil
	}

	randomHeight := minHeight + rand.Int63n(maxHeight-minHeight+1)
	log.Printf("[INFO] Selected height %d for finalized tipset comparison (range: %d-%d)", randomHeight, minHeight, maxHeight)

	// Connect to V2 nodes for finalized tipset comparison
	api11, closer11, err := ConnectToNodeV2(ctx, v2Nodes[0])
	if err != nil {
		log.Printf("[ERROR] Failed to connect to %s: %v", v2Nodes[0].Name, err)
		return nil
	}
	defer closer11()

	api22, closer22, err := ConnectToNodeV2(ctx, v2Nodes[1])
	if err != nil {
		log.Printf("[ERROR] Failed to connect to %s: %v", v2Nodes[1].Name, err)
		return nil
	}
	defer closer22()

	// Chain walk: Check 10 tipsets down from the selected height
	log.Printf("[INFO] Starting chain walk from height %d down to %d", randomHeight, randomHeight-9)

	for i := randomHeight; i >= randomHeight-9; i-- {
		log.Printf("[INFO] Checking finalized tipset at height %d", i)
		heightSelector := types.TipSetSelectors.Height(abi.ChainEpoch(i), true, types.TipSetAnchors.Finalized)

		ts1, err := api11.ChainGetTipSet(ctx, heightSelector)
		if err != nil {
			log.Printf("failed to get finalized tipset by height from %s: %v", v2Nodes[0].Name, err)
			return nil
		}
		log.Printf("[INFO] Finalized tipset %s on %s at height %d", ts1.Cids(), v2Nodes[0].Name, i)

		ts2, err := api22.ChainGetTipSet(ctx, heightSelector)
		if err != nil {
			log.Printf("failed to get finalized tipset by height from %s: %v", v2Nodes[1].Name, err)
			return nil
		}
		log.Printf("[INFO] Finalized tipset %s on %s at height %d", ts2.Cids(), v2Nodes[1].Name, i)

		assert.Always(ts1.Equals(ts2), "Chain synchronization test: Finalized tipset should always match",
			map[string]interface{}{
				"requirement": "Chain synchronization",
				"ts1":         ts1.Cids(),
				"ts2":         ts2.Cids(),
			})

		log.Printf("[INFO] Finalized tipsets %s match successfully at height %d", ts1.Cids(), i)
	}

	log.Printf("[INFO] Chain walk completed successfully - all 10 finalized tipsets match between nodes")
	return nil
}
