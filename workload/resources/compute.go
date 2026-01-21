package resources

import (
	"context"
	"fmt"
	"log"

	"github.com/filecoin-project/go-state-types/abi"
	lotusapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
)

const (
	// MinHeightForStateCheck is the minimum chain height required to perform state verification
	MinHeightForStateCheck = 20
	// StateCheckStartOffset is how many epochs behind head to start checking
	StateCheckStartOffset = 2
	// StateCheckEndOffset is how many epochs behind head to stop checking
	StateCheckEndOffset = 12
	// StateCheckTargetEpochs is the target number of epochs to verify
	StateCheckTargetEpochs = 10
)

// StateMismatch verifies state computation consistency by recomputing state
// for recent epochs and comparing against stored parent state roots.
func StateMismatch(ctx context.Context, node lotusapi.FullNode, nodeName string) error {
	checkTs, err := node.ChainHead(ctx)
	if err != nil {
		return fmt.Errorf("failed to get chain head: %w", err)
	}

	currentHeight := checkTs.Height()
	log.Printf("[INFO] Current chain height: %d", currentHeight)

	if currentHeight < MinHeightForStateCheck {
		log.Printf("[WARN] Chain height %d is less than %d epochs, skipping state check", currentHeight, MinHeightForStateCheck)
		return nil
	}

	startHeight := currentHeight - StateCheckStartOffset
	endHeight := currentHeight - StateCheckEndOffset

	log.Printf("[INFO] Checking state computation from height %d to %d (%d epochs)", startHeight, endHeight, StateCheckTargetEpochs)

	// Get tipset at start height
	startTs, err := node.ChainGetTipSetByHeight(ctx, startHeight, checkTs.Key())
	if err != nil {
		return fmt.Errorf("failed to get tipset at height %d: %w", startHeight, err)
	}
	checkTs = startTs

	epochsChecked := 0

	for epochsChecked < StateCheckTargetEpochs && checkTs.Height() >= endHeight {
		// Log progress every 2 epochs
		if epochsChecked%2 == 0 {
			log.Printf("[INFO] Checking epoch %d (checked %d/%d epochs)", checkTs.Height(), epochsChecked, StateCheckTargetEpochs)
		}

		// Get parent tipset
		execTsk := checkTs.Parents()
		execTs, err := node.ChainGetTipSet(ctx, execTsk)
		if err != nil {
			return fmt.Errorf("failed to get parent tipset at height %d: %w", checkTs.Height(), err)
		}

		// Stop if we've reached the end height
		if execTs.Height() < endHeight {
			log.Printf("[INFO] Reached end height %d, stopping state check", endHeight)
			break
		}

		// Compute state at parent height
		st, err := node.StateCompute(ctx, execTs.Height(), nil, execTsk)
		if err != nil {
			return fmt.Errorf("failed to compute state at height %d: %w", execTs.Height(), err)
		}

		// Verify state consistency
		stateMatches := st.Root == checkTs.ParentState()
		AssertAlways(nodeName, stateMatches,
			"State computation: Computed state must match parent state",
			map[string]interface{}{
				"exec_ts_height":  execTs.Height(),
				"check_ts_height": checkTs.Height(),
				"computed_root":   st.Root.String(),
				"expected_root":   checkTs.ParentState().String(),
				"epochs_checked":  epochsChecked,
			})

		if !stateMatches {
			return fmt.Errorf("state mismatch at height %d: computed %s, expected %s",
				execTs.Height(), st.Root.String(), checkTs.ParentState().String())
		}

		// Move to parent and increment counter
		checkTs = execTs
		epochsChecked++
	}

	log.Printf("[INFO] Completed state consistency check for %d epochs (from height %d to %d)",
		epochsChecked, startHeight, endHeight)
	return nil
}

// PerformStateCheck checks state consistency for a single node
func PerformStateCheck(ctx context.Context, nodeConfig *NodeConfig) error {
	log.Printf("[INFO] Starting state consistency check on node '%s'...", nodeConfig.Name)

	node, closer, err := ConnectToNode(ctx, *nodeConfig)
	if err != nil {
		return fmt.Errorf("failed to connect to node '%s': %w", nodeConfig.Name, err)
	}
	defer closer()

	return RetryOperation(ctx, func() error {
		return StateMismatch(ctx, node, nodeConfig.Name)
	}, "State consistency check operation")
}

const (
	// CrossNodeStateCheckEpochs is how many epochs to walk back when comparing state
	CrossNodeStateCheckEpochs = 10
)

// nodeState holds a node's API and its current tipset during state comparison
type nodeState struct {
	name   string
	api    CommonAPI
	tipset *types.TipSet
	closer func()
}

// CompareStateAcrossNodes gets common finalized tipset from all nodes,
// walks backwards comparing parent state roots, and asserts on any inconsistencies.
func CompareStateAcrossNodes(ctx context.Context, nodes []NodeConfig) error {
	if len(nodes) < 2 {
		log.Println("[WARN] Need at least 2 nodes to compare state")
		return nil
	}

	// Connect to all nodes using CommonAPI
	var nodeStates []*nodeState
	for _, nodeConfig := range nodes {
		api, closer, err := ConnectToCommonNode(ctx, nodeConfig)
		if err != nil {
			// Clean up already-connected nodes
			for _, ns := range nodeStates {
				ns.closer()
			}
			return fmt.Errorf("failed to connect to node %s: %w", nodeConfig.Name, err)
		}
		nodeStates = append(nodeStates, &nodeState{
			name:   nodeConfig.Name,
			api:    api,
			closer: closer,
		})
	}
	defer func() {
		for _, ns := range nodeStates {
			ns.closer()
		}
	}()

	// Get common finalized tipset
	var apis []CommonAPI
	for _, ns := range nodeStates {
		apis = append(apis, ns.api)
	}

	commonTipset, err := GetCommonTipSet(ctx, apis)
	if err != nil {
		return fmt.Errorf("failed to get common tipset: %w", err)
	}

	log.Printf("[INFO] Starting cross-node state comparison from finalized height %d", commonTipset.Height())

	// Initialize all nodes with the common tipset
	for _, ns := range nodeStates {
		ns.tipset = commonTipset
	}

	// Walk backwards comparing state
	epochsChecked := 0
	for epochsChecked < CrossNodeStateCheckEpochs {
		currentHeight := nodeStates[0].tipset.Height()
		if currentHeight == 0 {
			log.Println("[INFO] Reached genesis, stopping state comparison")
			break
		}

		// Collect parent state roots from all nodes
		stateRoots := make(map[string][]string) // stateRoot -> []nodeName
		for _, ns := range nodeStates {
			parentState := ns.tipset.ParentState().String()
			stateRoots[parentState] = append(stateRoots[parentState], ns.name)
		}

		// Check for consistency
		statesMatch := len(stateRoots) == 1
		AssertAlways("CrossNodeState", statesMatch,
			"Cross-node state comparison: All nodes must have same parent state root",
			map[string]interface{}{
				"height":       currentHeight,
				"state_roots":  stateRoots,
				"epoch_number": epochsChecked,
			})

		if !statesMatch {
			var rootDetails []string
			for root, nodeNames := range stateRoots {
				rootDetails = append(rootDetails, fmt.Sprintf("%s: %v", root, nodeNames))
			}
			return fmt.Errorf("state mismatch at height %d: %v", currentHeight, rootDetails)
		}

		log.Printf("[INFO] Height %d: All %d nodes have consistent state (%d/%d epochs)",
			currentHeight, len(nodeStates), epochsChecked+1, CrossNodeStateCheckEpochs)

		// Move all nodes to parent tipset
		for _, ns := range nodeStates {
			parentKey := ns.tipset.Parents()
			parentTs, err := ns.api.ChainGetTipSet(ctx, parentKey.Cids())
			if err != nil {
				return fmt.Errorf("failed to get parent tipset for %s at height %d: %w",
					ns.name, currentHeight, err)
			}
			ns.tipset = parentTs
		}

		epochsChecked++
	}

	log.Printf("[INFO] Cross-node state comparison completed: %d epochs verified, all nodes consistent", epochsChecked)
	return nil
}

// PerformCrossNodeStateCheck runs cross-node state comparison with retry logic
func PerformCrossNodeStateCheck(ctx context.Context, config *Config) error {
	log.Println("[INFO] Starting cross-node state comparison...")

	return RetryOperation(ctx, func() error {
		return CompareStateAcrossNodes(ctx, config.Nodes)
	}, "Cross-node state comparison")
}

// CompareStateAtHeight compares state across all nodes at a specific height
func CompareStateAtHeight(ctx context.Context, nodes []NodeConfig, height abi.ChainEpoch) error {
	if len(nodes) < 2 {
		return fmt.Errorf("need at least 2 nodes to compare state")
	}

	log.Printf("[INFO] Comparing state across %d nodes at height %d", len(nodes), height)

	stateRoots := make(map[string][]string) // stateRoot -> []nodeName

	for _, nodeConfig := range nodes {
		api, closer, err := ConnectToCommonNode(ctx, nodeConfig)
		if err != nil {
			return fmt.Errorf("failed to connect to %s: %w", nodeConfig.Name, err)
		}

		head, err := api.ChainHead(ctx)
		if err != nil {
			closer()
			return fmt.Errorf("failed to get chain head from %s: %w", nodeConfig.Name, err)
		}

		tipset, err := api.ChainGetTipSetByHeight(ctx, int64(height), head.Key().Cids())
		closer()

		if err != nil {
			return fmt.Errorf("failed to get tipset at height %d from %s: %w", height, nodeConfig.Name, err)
		}

		parentState := tipset.ParentState().String()
		stateRoots[parentState] = append(stateRoots[parentState], nodeConfig.Name)
	}

	statesMatch := len(stateRoots) == 1
	AssertAlways("StateAtHeight", statesMatch,
		fmt.Sprintf("State at height %d: All nodes must have same parent state root", height),
		map[string]interface{}{
			"height":      height,
			"state_roots": stateRoots,
		})

	if !statesMatch {
		return fmt.Errorf("state mismatch at height %d: nodes disagree on parent state", height)
	}

	log.Printf("[INFO] All nodes have consistent state at height %d", height)
	return nil
}
