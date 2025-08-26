package resources

import (
	"context"
	"log"

	"github.com/antithesishq/antithesis-sdk-go/assert"
	"github.com/filecoin-project/lotus/api"
)

func StateMismatch(ctx context.Context, api api.FullNode) error {
	checkTs, err := api.ChainHead(ctx)
	if err != nil {
		log.Printf("[ERROR] Failed to get chain head: %v", err)
		return err
	}

	currentHeight := checkTs.Height()
	log.Printf("[INFO] Current chain height: %d", currentHeight)

	// Ensure we have at least 20 epochs to work with
	if currentHeight < 20 {
		log.Printf("[WARN] Chain height %d is less than 20 epochs, skipping state check", currentHeight)
		return nil
	}

	startHeight := currentHeight - 2
	endHeight := currentHeight - 12

	log.Printf("[INFO] Checking state computation from height %d to %d (8 epochs)", startHeight, endHeight)

	// Get tipset at start height (height-2)
	startTs, err := api.ChainGetTipSetByHeight(ctx, startHeight, checkTs.Key())
	if err != nil {
		log.Printf("[ERROR] Failed to get tipset at height %d: %v", startHeight, err)
		return err
	}
	checkTs = startTs

	epochsChecked := 0
	targetEpochs := 10

	for epochsChecked < targetEpochs && checkTs.Height() >= endHeight {
		if checkTs == nil {
			log.Printf("[ERROR] checkTs is nil at height %d", checkTs.Height())
			return nil
		}

		// Log progress every 2 epochs
		if epochsChecked%2 == 0 {
			log.Printf("[INFO] Checking epoch %d (checked %d/%d epochs)", checkTs.Height(), epochsChecked, targetEpochs)
		}

		// Get parent tipset
		execTsk := checkTs.Parents()
		execTs, err := api.ChainGetTipSet(ctx, execTsk)
		if err != nil {
			log.Printf("[ERROR] Failed to get tipset at height %d: %v", checkTs.Height(), err)
			return err
		}
		if execTs == nil {
			log.Printf("[ERROR] Got nil tipset for parents at height %d", checkTs.Height())
			return nil
		}

		// Stop if we've reached the end height
		if execTs.Height() < endHeight {
			log.Printf("[INFO] Reached end height %d, stopping state check", endHeight)
			break
		}

		// Compute state at parent height
		st, err := api.StateCompute(ctx, execTs.Height(), nil, execTsk)
		if err != nil {
			log.Printf("[ERROR] Failed to compute state at height %d: %v", execTs.Height(), err)
			return err
		}

		// Verify state consistency
		if st.Root != checkTs.ParentState() {
			assert.Always(st.Root == checkTs.ParentState(),
				"[State Consistency] Computed state must match parent state",
				map[string]interface{}{
					"exec_ts_height":  execTs.Height(),
					"check_ts_height": checkTs.Height(),
					"exec_ts_root":    st.Root.String(),
					"check_ts_root":   checkTs.ParentState().String(),
					"property":        "State computation consistency",
					"impact":          "Critical - indicates state computation error",
					"details":         "Computed state root must match parent state root",
					"recommendation":  "Check state computation logic and tipset traversal",
					"epochs_checked":  epochsChecked,
					"target_epochs":   targetEpochs,
					"start_height":    startHeight,
					"end_height":      endHeight,
					"current_height":  currentHeight,
				})
			return err
		}

		// Move to parent and increment counter
		checkTs = execTs
		epochsChecked++
	}

	log.Printf("[INFO] Completed state consistency check for %d epochs (from height %d to %d)",
		epochsChecked, startHeight, endHeight)
	return nil
}

// PerformStateCheck checks state consistency
func PerformStateCheck(ctx context.Context, nodeConfig *NodeConfig) error {
	log.Printf("[INFO] Starting state consistency check on node '%s'...", nodeConfig.Name)

	api, closer, err := ConnectToNode(ctx, *nodeConfig)
	if err != nil {
		log.Printf("[ERROR] Failed to connect to Lotus node '%s': %v", nodeConfig.Name, err)
		return nil
	}
	defer closer()

	return RetryOperation(ctx, func() error {
		return StateMismatch(ctx, api)
	}, "State consistency check operation")
}
