package resources

import (
	"context"
	"encoding/json"
	"fmt"
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
		return nil, fmt.Errorf("need at least 2 Lotus nodes for consensus checking")
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
		return nil, fmt.Errorf("RPC error: %s", rpcResp.Error.Message)
	}

	return &rpcResp, nil
}

// getChainHead gets the current chain head for a node
// It handles both V1 and V2 API versions
func (cc *ConsensusChecker) getChainHead(ctx context.Context, nodeInfo NodeInfo) (abi.ChainEpoch, error) {
	// Use different method names for V1 and V2 APIs
	methodName := "Filecoin.ChainHead"
	if strings.Contains(nodeInfo.RPCURL, "/rpc/v2") {
		methodName = "Filecoin.ChainGetHead"
	}

	resp, err := cc.makeRPCRequest(ctx, nodeInfo, methodName, []interface{}{})
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
				result.err = fmt.Errorf("failed to get tipset at height %d from %s: %w", height, name, err)
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
		return false, tipsetKeys, fmt.Errorf("errors during consensus check at height %d: %v", height, errors)
	}

	return len(tipsetKeys) == 1, tipsetKeys, nil
}

// CheckConsensus verifies that all nodes have consensus on tipsets for a range of heights
// If no height is specified (height = 0), it chooses a random height between genesis and minHead-20
func (cc *ConsensusChecker) CheckConsensus(ctx context.Context, height abi.ChainEpoch) error {
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
			return fmt.Errorf("failed to get chain head from %s: %w", nodeName, err)
		}
		if first || head < minHead {
			minHead = head
			first = false
		}
	}

	// Choose a random height between genesis (1) and minHead-20 (to avoid reorgs)
	if height == 0 {
		if minHead < 30 {
			return fmt.Errorf("chain too short for consensus check, height: %d", minHead)
		}
		safeMaxHeight := minHead - 20 // Stay away from head to avoid reorgs
		height = abi.ChainEpoch(rand.Int63n(int64(safeMaxHeight-1))) + 1
	} else if height > minHead {
		log.Printf("Requested height %d is beyond current chain head %d, will use random height instead", height, minHead)
		height = 0
		if minHead < 30 {
			return fmt.Errorf("chain too short for consensus check, height: %d", minHead)
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
}
