package resources

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"

	"github.com/filecoin-project/go-state-types/abi"
)

const (
	// ConsensusRetryDelay is the time to wait between retries
	ConsensusRetryDelay = 5 * time.Second
	// ConsensusSettlingPeriod is the time to wait for tipsets to settle
	ConsensusSettlingPeriod = 10 * time.Second
	// MinHeightForConsensus is the minimum chain height required
	MinHeightForConsensusCheck = 30
	// ConsensusHeightBuffer is how far from head to stay to avoid reorgs
	ConsensusHeightBuffer = 20
	// ConsensusWalkEpochs is how many epochs to walk back and check
	ConsensusWalkEpochs = 10
)

// ConsensusChecker checks consensus across multiple nodes using CommonAPI
type ConsensusChecker struct {
	nodes   []NodeConfig
	apis    []CommonAPI
	closers []func()
}

// NewConsensusChecker creates a new consensus checker for the given nodes
func NewConsensusChecker(ctx context.Context, nodes []NodeConfig) (*ConsensusChecker, error) {
	if len(nodes) < 2 {
		return nil, fmt.Errorf("need at least 2 nodes for consensus checking, got %d", len(nodes))
	}

	checker := &ConsensusChecker{
		nodes: nodes,
	}

	// Connect to all nodes
	for _, node := range nodes {
		api, closer, err := ConnectToCommonNode(ctx, node)
		if err != nil {
			// Clean up already connected
			for _, c := range checker.closers {
				c()
			}
			return nil, fmt.Errorf("failed to connect to %s: %w", node.Name, err)
		}
		checker.apis = append(checker.apis, api)
		checker.closers = append(checker.closers, closer)
	}

	log.Printf("[INFO] Consensus checker initialized with %d nodes", len(nodes))
	return checker, nil
}

// Close closes all connections
func (cc *ConsensusChecker) Close() {
	for _, closer := range cc.closers {
		closer()
	}
}

// getMinHeight gets the minimum chain height across all nodes
func (cc *ConsensusChecker) getMinHeight(ctx context.Context) (abi.ChainEpoch, error) {
	var minHeight abi.ChainEpoch
	first := true

	for i, api := range cc.apis {
		head, err := api.ChainHead(ctx)
		if err != nil {
			return 0, fmt.Errorf("failed to get chain head from %s: %w", cc.nodes[i].Name, err)
		}
		if first || head.Height() < minHeight {
			minHeight = head.Height()
			first = false
		}
	}

	return minHeight, nil
}

// checkTipsetConsensus checks if all nodes agree on the tipset at a given height
func (cc *ConsensusChecker) checkTipsetConsensus(ctx context.Context, height abi.ChainEpoch) (bool, map[string][]string, error) {
	var wg sync.WaitGroup
	type nodeResult struct {
		name      string
		tipsetKey string
		err       error
	}

	results := make(chan nodeResult, len(cc.apis))

	for i, api := range cc.apis {
		wg.Add(1)
		go func(idx int, nodeAPI CommonAPI) {
			defer wg.Done()
			result := nodeResult{name: cc.nodes[idx].Name}

			head, err := nodeAPI.ChainHead(ctx)
			if err != nil {
				result.err = err
				results <- result
				return
			}

			tipset, err := nodeAPI.ChainGetTipSetByHeight(ctx, int64(height), head.Key().Cids())
			if err != nil {
				result.err = err
				results <- result
				return
			}

			result.tipsetKey = tipset.Key().String()
			results <- result
		}(i, api)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	tipsetKeys := make(map[string][]string) // tipsetKey -> []nodeName
	var errors []error

	for result := range results {
		if result.err != nil {
			errors = append(errors, fmt.Errorf("%s: %w", result.name, result.err))
			continue
		}
		tipsetKeys[result.tipsetKey] = append(tipsetKeys[result.tipsetKey], result.name)
	}

	if len(errors) > 0 {
		log.Printf("[WARN] Errors during consensus check at height %d: %v", height, errors)
	}

	return len(tipsetKeys) == 1 && len(errors) == 0, tipsetKeys, nil
}

// selectCheckHeight selects an appropriate height for consensus checking
func (cc *ConsensusChecker) selectCheckHeight(ctx context.Context, requestedHeight abi.ChainEpoch) (abi.ChainEpoch, error) {
	minHead, err := cc.getMinHeight(ctx)
	if err != nil {
		return 0, err
	}

	if minHead < MinHeightForConsensusCheck {
		return 0, fmt.Errorf("chain too short for consensus check: height %d < %d", minHead, MinHeightForConsensusCheck)
	}

	// If height specified and valid, use it
	if requestedHeight > 0 && requestedHeight <= minHead-ConsensusHeightBuffer {
		return requestedHeight, nil
	}

	// Otherwise pick a random height in safe range
	safeMaxHeight := minHead - ConsensusHeightBuffer
	if safeMaxHeight < ConsensusWalkEpochs {
		return ConsensusWalkEpochs, nil
	}

	return abi.ChainEpoch(rand.Int63n(int64(safeMaxHeight-ConsensusWalkEpochs))) + ConsensusWalkEpochs, nil
}

// CheckConsensus verifies that all nodes agree on tipsets for a range of heights
func (cc *ConsensusChecker) CheckConsensus(ctx context.Context, requestedHeight abi.ChainEpoch) error {
	height, err := cc.selectCheckHeight(ctx, requestedHeight)
	if err != nil {
		return err
	}

	log.Printf("[INFO] Starting consensus walk from height %d", height)

	// Wait for settling period
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(ConsensusSettlingPeriod):
	}

	// Check consensus for consecutive tipsets
	for i := 0; i < ConsensusWalkEpochs; i++ {
		currentHeight := height - abi.ChainEpoch(i)
		if currentHeight < 1 {
			break
		}

		log.Printf("[INFO] Checking consensus at height %d (%d/%d)", currentHeight, i+1, ConsensusWalkEpochs)

		consensusReached, tipsetKeys, err := cc.checkTipsetConsensus(ctx, currentHeight)
		if err != nil {
			return fmt.Errorf("consensus check failed at height %d: %w", currentHeight, err)
		}

		AssertAlways("ConsensusChecker", consensusReached,
			"Consensus check: All nodes must agree on the same tipset",
			map[string]interface{}{
				"height":         currentHeight,
				"tipset_keys":    tipsetKeys,
				"unique_tipsets": len(tipsetKeys),
				"nodes_checked":  len(cc.nodes),
			})

		if !consensusReached {
			return fmt.Errorf("consensus not reached at height %d: %v", currentHeight, tipsetKeys)
		}
	}

	log.Printf("[INFO] Consensus walk completed: heights %d to %d verified", height, height-abi.ChainEpoch(ConsensusWalkEpochs-1))
	return nil
}

// PerformConsensusCheck checks consensus between nodes
func PerformConsensusCheck(ctx context.Context, config *Config, height int64) error {
	log.Printf("[INFO] Starting consensus check...")

	checker, err := NewConsensusChecker(ctx, config.Nodes)
	if err != nil {
		return fmt.Errorf("failed to create consensus checker: %w", err)
	}
	defer checker.Close()

	return RetryOperation(ctx, func() error {
		return checker.CheckConsensus(ctx, abi.ChainEpoch(height))
	}, "Consensus check operation")
}
