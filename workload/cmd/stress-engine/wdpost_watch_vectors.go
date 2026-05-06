package main

import (
	"sync"

	"github.com/antithesishq/antithesis-sdk-go/assert"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	builtintypes "github.com/filecoin-project/go-state-types/builtin"
	"github.com/filecoin-project/go-state-types/exitcode"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/actors/builtin"
	"github.com/filecoin-project/lotus/chain/types"
)

// ===========================================================================
// DoWindowPoStAudit — WindowPoSt Submission Sanity
//
// Targets: under partition the lotus-miner can submit a
// SubmitWindowedPoSt with a deadline that's stale relative to current head,
// and the miner actor rejects it with ErrIllegalArgument(16) ("invalid
// deadline N at epoch M, expected K").
//
// We can't read the lotus-miner side directly from the workload, but the
// failure leaves a permanent on-chain trace: an applied SubmitWindowedPoSt
// message with a non-zero exit code. We walk a short window of recent
// finalized tipsets and check every (msg, receipt) pair where:
//   - To is a storage miner actor
//   - Method == MethodsMiner.SubmitWindowedPoSt (5)
// and assert that the receipt exit code is never ErrIllegalArgument
// (the specific deadline-mismatch failure).
//
// State is kept across invocations so we don't double-count the same
// receipt: wdpostAudited tracks (tsk, msg cid) pairs already inspected.
// ===========================================================================

const wdpostAuditWindow = abi.ChainEpoch(20) // tipsets back from finalized to scan

var (
	wdpostAudited   = make(map[string]struct{})
	wdpostAuditedMu sync.Mutex

	// Cached miner-actor classification: addr -> isMiner. The miner set
	// rarely changes during a run, but slashing can flip it; on cache
	// miss we re-resolve, so the set self-heals.
	minerActorCache   = make(map[address.Address]bool)
	minerActorCacheMu sync.Mutex
)

func DoWindowPoStAudit() {
	if len(nodeKeys) < 1 {
		return
	}
	if !allNodesPastEpoch(f3MinEpoch) {
		return
	}
	// During an active partition the failure mode is expected (the bug is
	// about that very window). Wait until heal+convergence; if the bug
	// triggered, the failed receipt is permanent in the chain anyway.
	if partitionActive.Load() {
		return
	}

	node, nodeName := pickLotusNode()
	if node == nil {
		return
	}

	finH, finTsk := getFinalizedHeight()
	if finH < finalizedMinHeight {
		return
	}

	ts, err := node.ChainGetTipSet(ctx, finTsk)
	if err != nil {
		debugLog("[wdpost-audit] %s: ChainGetTipSet(%s) failed: %v", nodeName, finTsk, err)
		return
	}

	// Walk back a short window of finalized tipsets. ChainGetParentMessages
	// returns the messages applied *into* the given tipset's parent state.
	for i := abi.ChainEpoch(0); i < wdpostAuditWindow && ts != nil; i++ {
		auditTipsetWdpost(node, ts)

		if len(ts.Blocks()) == 0 {
			break
		}
		parents := ts.Parents()
		if parents.IsEmpty() {
			break
		}
		next, err := node.ChainGetTipSet(ctx, parents)
		if err != nil {
			debugLog("[wdpost-audit] parent walk failed: %v", err)
			break
		}
		ts = next
	}
}

func auditTipsetWdpost(node api.FullNode, ts *types.TipSet) {
	if len(ts.Blocks()) == 0 {
		return
	}
	anchorBlock := ts.Blocks()[0].Cid()

	msgs, err := node.ChainGetParentMessages(ctx, anchorBlock)
	if err != nil {
		debugLog("[wdpost-audit] ChainGetParentMessages failed: %v", err)
		return
	}
	receipts, err := node.ChainGetParentReceipts(ctx, anchorBlock)
	if err != nil {
		debugLog("[wdpost-audit] ChainGetParentReceipts failed: %v", err)
		return
	}
	if len(msgs) != len(receipts) {
		debugLog("[wdpost-audit] msg/receipt length mismatch: %d vs %d", len(msgs), len(receipts))
		return
	}

	tskHex := ts.Key().String()
	for i, m := range msgs {
		if m.Message.Method != builtintypes.MethodsMiner.SubmitWindowedPoSt {
			continue
		}
		if !isMinerActor(node, m.Message.To) {
			continue
		}

		key := tskHex + "|" + m.Cid.String()
		wdpostAuditedMu.Lock()
		if _, seen := wdpostAudited[key]; seen {
			wdpostAuditedMu.Unlock()
			continue
		}
		wdpostAudited[key] = struct{}{}
		wdpostAuditedMu.Unlock()

		exit := receipts[i].ExitCode
		details := map[string]any{
			"miner":          m.Message.To.String(),
			"from":           m.Message.From.String(),
			"msg_cid":        m.Cid.String(),
			"applied_in_tsk": tskHex,
			"applied_height": ts.Height(),
			"exit_code":      int64(exit),
		}

		// Always: a SubmitWindowedPoSt accepted by the message-layer must
		// not fail at apply with ErrIllegalArgument. That specific exit
		// code is the stale-deadline signature.
		assert.Always(exit != exitcode.ErrIllegalArgument,
			"WindowPoSt submission not rejected with ErrIllegalArgument (stale deadline)",
			details)

		if exit == 0 {
			assert.Sometimes(true, "WindowPoSt submission accepted (exit=0)", details)
		}
	}
}

func isMinerActor(node api.FullNode, addr address.Address) bool {
	minerActorCacheMu.Lock()
	if v, ok := minerActorCache[addr]; ok {
		minerActorCacheMu.Unlock()
		return v
	}
	minerActorCacheMu.Unlock()

	actor, err := node.StateGetActor(ctx, addr, types.EmptyTSK)
	if err != nil || actor == nil {
		return false
	}
	v := builtin.IsStorageMinerActor(actor.Code)

	minerActorCacheMu.Lock()
	minerActorCache[addr] = v
	minerActorCacheMu.Unlock()
	return v
}
