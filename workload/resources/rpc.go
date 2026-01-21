package resources

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/filecoin-project/lotus/chain/types"
)

// Helper to cast interface{} to *types.TipSet
// Since generated code might return interface{} (map[string]interface{}), we might need to marshal/unmarshal if strictly necessary,
// or trust that jsonrpc unmarshals into the struct if we provided a typed struct to the client.
// Wait, if the interface definition in CommonAPI says `interface{}`, the jsonrpc client unmarshals into `map[string]interface{}`.
// If it says `*types.TipSet`, it unmarshals into `*types.TipSet`.
// I MUST ensure the generated code uses `*types.TipSet`.
// If I verified generation works, I can use direct types.

// GetCommonTipSet polls all provided API clients for their finalized tipset
// and returns the latest tipset that all nodes agree upon (same CIDs at same Height).
func GetCommonTipSet(ctx context.Context, apis []CommonAPI) (*types.TipSet, error) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	log.Println("[INFO] Waiting for common finalized tipset across all nodes...")

	for {
		select {
		case <-timeoutCtx.Done():
			return nil, fmt.Errorf("timeout waiting for common tipset")
		case <-ticker.C:
			var heads []*types.TipSet
			success := true
			for i, api := range apis {
				// We expect generated code to return *types.TipSet now
				ts, err := api.ChainGetFinalizedTipSet(ctx)
				if err != nil {
					log.Printf("[WARN] Node %d ChainGetFinalizedTipSet failed: %v", i, err)
					success = false
					break
				}
				// If generated code returns interface{}, we'd get a compile error here unless we cast.
				// I will modify this file AFTER checking gen_api.go to see if I need casting.
				// For now, I'm writing this assuming it IS typed.
				// BUT if it's interface{}, I'll change the generated code or use a helper.

				if ts == nil {
					log.Printf("[WARN] Node %d returned nil tipset", i)
					success = false
					break
				}
				heads = append(heads, ts)
			}

			if !success {
				continue
			}

			if len(heads) == 0 {
				continue
			}

			minHeight := heads[0].Height()
			for _, ts := range heads[1:] {
				if ts.Height() < minHeight {
					minHeight = ts.Height()
				}
			}

			var targetTipSets []*types.TipSet
			fetchSuccess := true
			for i, api := range apis {
				head := heads[i]
				if head.Height() == minHeight {
					targetTipSets = append(targetTipSets, head)
				} else {
					// Use keys from the head to look back? specific key?
					// ChainGetTipSetByHeight takes (ctx, height, tsk).
					// The TSK argument is primarily for looking up... wait, typically it's the *head* to search back from.
					// So passing heads[i].Key() is correct.
					ts, err := api.ChainGetTipSetByHeight(ctx, int64(minHeight), head.Key().Cids())
					if err != nil {
						log.Printf("[WARN] Node %d ChainGetTipSetByHeight failed: %v", i, err)
						fetchSuccess = false
						break
					}
					// Assuming strictly typed return
					targetTipSets = append(targetTipSets, ts)
				}
			}

			if !fetchSuccess {
				continue
			}

			firstKey := targetTipSets[0].Key()
			allMatch := true
			for i, ts := range targetTipSets[1:] {
				// Use String() comparison for robust key equality
				if ts.Key().String() != firstKey.String() {
					log.Printf("[INFO] Mismatch at height %d: Node 0 has %s, Node %d has %s", minHeight, firstKey, i+1, ts.Key())
					allMatch = false
					break
				}
			}

			if allMatch {
				log.Printf("[INFO] Common finalized tipset found: %s at height %d", firstKey, minHeight)
				return targetTipSets[0], nil
			}
		}
	}
}
