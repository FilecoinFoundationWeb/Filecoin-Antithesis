package resources

import (
	"context"
	"log"
	"math/rand"

	"github.com/antithesishq/antithesis-sdk-go/assert"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
)

// StateMismatch verifies state computation consistency by checking if computed state
// matches parent state at a random height. It walks back through the chain from the
// selected height to genesis, verifying state computation at each step.
func StateMismatch(ctx context.Context, api api.FullNode) error {
	checkTs, err := api.ChainHead(ctx)
	if err != nil {
		log.Printf("[ERROR] Failed to get chain head: %v", err)
		return err
	}

	maxHeight := checkTs.Height()
	randomHeight := abi.ChainEpoch(rand.Int63n(int64(maxHeight)))
	log.Printf("[INFO] Checking state mismatch at random height %d (between 0 and %d)", randomHeight, maxHeight)

	// Get tipset at random height
	randomTs, err := api.ChainGetTipSetByHeight(ctx, randomHeight, checkTs.Key())
	if err != nil {
		log.Printf("[ERROR] Failed to get tipset at height %d: %v", randomHeight, err)
		return err
	}
	checkTs = randomTs

	for checkTs.Height() != 0 {
		if checkTs == nil {
			log.Printf("[ERROR] checkTs is nil")
			return nil
		}
		if checkTs.Height()%1000 == 0 {
			log.Printf("Reached height %d", checkTs.Height())
		}
		execTsk := checkTs.Parents()
		execTs, err := api.ChainGetTipSet(ctx, execTsk)
		if err != nil {
			log.Printf("[ERROR] Failed to get tipset at height %d: %v", checkTs.Height(), err)
			return nil
		}
		if execTs == nil {
			log.Printf("[ERROR] Got nil tipset for parents at height %d", checkTs.Height())
			return nil
		}
		st, err := api.StateCompute(ctx, execTs.Height(), nil, execTsk)
		if err != nil {
			log.Printf("[ERROR] Failed to compute state at height %d: %v", execTs.Height(), err)
			return err
		}
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
				})
			return err
		}
		checkTs = execTs
	}
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
