package main

import (
	"strings"

	"github.com/antithesishq/antithesis-sdk-go/assert"
)

// ===========================================================================
// DoChainStoreSelfHead — Blockstore Recovery After Restart
//
// Targets: Lotus badger blockstore loses IPLD blocks on
// unclean shutdown, so after Antithesis kills+restarts a node it can come
// back unable to resolve its own chain head's parents/messages/receipts.
//
// For each responsive node, fetch its self-reported chain head, then walk it:
//  1. ChainGetTipSet on the head's TSK   (header lookup)
//  2. ChainGetBlockMessages on first block (message AMT lookup)
//  3. ChainGetParentReceipts on the TSK   (receipt AMT lookup)
//  4. ChainReadObj on the block's ParentStateRoot (state-tree HAMT lookup)
//
// Any "ipld: could not find" / "block was not found locally" failure means
// the node lost a block its own canonical chain references — that is the
// blockstore corruption signature. Everything else (RPC unavailable, ctx
// timeout) is skipped, not failed.
// ===========================================================================

// ipldMissing is the substring lotus emits when the badger blockstore can't
// resolve a CID it should have. We string-match because lotus does not
// surface a sentinel error for this case across the affected RPCs.
func ipldMissing(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "ipld: could not find") ||
		strings.Contains(s, "block was not found locally") ||
		strings.Contains(s, "blockstore: block not found")
}

func DoChainStoreSelfHead() {
	// Heal lag is normal: a node that just rejoined may not have all the
	// state-tree entries for its own head yet. The bug we want to catch
	// is one that persists steady-state, so skip during active partitions.
	if partitionActive.Load() {
		return
	}

	for _, name := range nodeKeys {
		node := nodes[name]

		head, err := node.ChainHead(ctx)
		if err != nil {
			debugLog("[chainstore-self] %s: ChainHead failed: %v", name, err)
			continue
		}
		if len(head.Blocks()) == 0 {
			continue
		}

		// 1. Header — re-resolve the head TSK
		_, hdrErr := node.ChainGetTipSet(ctx, head.Key())
		hdrMissing := ipldMissing(hdrErr)

		// 2. Messages — fetch the message AMT for the first block in the head
		firstBlk := head.Blocks()[0]
		_, msgErr := node.ChainGetBlockMessages(ctx, firstBlk.Cid())
		msgMissing := ipldMissing(msgErr)

		// 3. Receipts — pull parent receipts AMT through the head TSK
		_, rcptErr := node.ChainGetParentReceipts(ctx, firstBlk.Cid())
		rcptMissing := ipldMissing(rcptErr)

		// 4. State tree — raw IPLD read of the block's parent state root
		_, stateErr := node.ChainReadObj(ctx, firstBlk.ParentStateRoot)
		stateMissing := ipldMissing(stateErr)

		anyMissing := hdrMissing || msgMissing || rcptMissing || stateMissing

		details := map[string]any{
			"node":              name,
			"head_height":       head.Height(),
			"head_tsk":          head.Key().String(),
			"missing_header":    hdrMissing,
			"missing_messages":  msgMissing,
			"missing_receipts":  rcptMissing,
			"missing_state":     stateMissing,
			"err_header":        errStr(hdrErr),
			"err_messages":      errStr(msgErr),
			"err_receipts":      errStr(rcptErr),
			"err_state":         errStr(stateErr),
		}

		// Always: the node's own canonical chain head must be fully
		// resolvable in its local blockstore. Anything else means the
		// blockstore lost data it acknowledged as final.
		assert.Always(!anyMissing,
			"node can resolve its own chain head from local blockstore",
			details)
	}
}
