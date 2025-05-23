package resources

import (
	"context"
	"log"
	"math/rand"

	"github.com/antithesishq/antithesis-sdk-go/assert"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
)

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
		if checkTs.Height()%1000 == 0 {
			log.Printf("Reached height %d", checkTs.Height())
		}
		execTsk := checkTs.Parents()
		execTs, err := api.ChainGetTipSet(ctx, execTsk)
		if err != nil {
			log.Printf("[ERROR] Failed to get tipset at height %d: %v", execTs.Height(), err)
			return err
		}
		st, err := api.StateCompute(ctx, execTs.Height(), nil, execTsk)
		if err != nil {
			log.Printf("[ERROR] Failed to compute state at height %d: %v", execTs.Height(), err)
			return err
		}
		if st.Root != checkTs.ParentState() {
			assert.Always(st.Root == checkTs.ParentState(), "State mismatch at height %d", map[string]any{
				"exec_ts_height":  execTs.Height(),
				"check_ts_height": checkTs.Height(),
				"exec_ts_root":    st.Root,
				"check_ts_root":   checkTs.ParentState(),
			})
			return err
		}
		checkTs = execTs
	}
	return nil
}
