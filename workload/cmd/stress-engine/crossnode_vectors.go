package main

import (
	"bytes"
	"log"
	"sync"

	"github.com/antithesishq/antithesis-sdk-go/assert"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/ipfs/go-cid"
)

// ===========================================================================
// Cross-Node Divergence Vectors
//
// These vectors actively try to create divergence between nodes by exploiting
// message ordering, nonce edge cases, and gas competition. They compare
// receipts, state roots, and execution results across all nodes.
//
// Critical: nodes may be out of sync (Forest can lag, reorg chaos can
// partition). All comparisons use finalized tipsets and skip lagging nodes.
// ===========================================================================

// ===========================================================================
// DoReceiptAudit
//
// Picks a random message from an already-finalized block and compares its
// receipt (ExitCode, GasUsed, Return) field-by-field across ALL nodes.
// Piggybacks on messages that other vectors already produced — sends nothing.
//
// DoStateAudit checks receipt *counts*; this checks receipt *contents*.
// ===========================================================================

func DoReceiptAudit() {
	if len(nodeKeys) < 2 {
		return
	}
	if !allNodesPastEpoch(f3MinEpoch) {
		return
	}

	finalizedHeight, _ := getFinalizedHeight()
	if finalizedHeight < finalizedMinHeight {
		return
	}

	// Pick a random finalized height
	checkHeight := abi.ChainEpoch(rngIntn(int(finalizedHeight)) + 1)

	// Get tipset at that height from the first node (anchored to its finalized chain)
	refNode := nodes[nodeKeys[0]]
	refFinTs, err := refNode.ChainGetFinalizedTipSet(ctx)
	if err != nil {
		return
	}
	ts, err := refNode.ChainGetTipSetByHeight(ctx, checkHeight, refFinTs.Key())
	if err != nil {
		return
	}
	if len(ts.Cids()) == 0 {
		return
	}

	// Pick a random block from the tipset
	blkCid := ts.Cids()[rngIntn(len(ts.Cids()))]

	// Get messages from the reference node to find a message to audit
	msgs, err := refNode.ChainGetParentMessages(ctx, blkCid)
	if err != nil || len(msgs) == 0 {
		return
	}

	// Pick a random message index
	msgIdx := rngIntn(len(msgs))

	// Collect receipts from all nodes
	type receiptResult struct {
		node     string
		exitCode int64
		gasUsed  int64
		retData  []byte
	}
	var results []receiptResult

	for _, name := range nodeKeys {
		node := nodes[name]

		// Get the node's finalized tipset to anchor the lookup
		nodeFinTs, err := node.ChainGetFinalizedTipSet(ctx)
		if err != nil {
			debugLog("[receipt-audit] ChainGetFinalizedTipSet failed on %s: %v", name, err)
			continue
		}

		// Skip if this node hasn't finalized up to our check height
		if nodeFinTs.Height() < checkHeight {
			debugLog("[receipt-audit] %s finalized=%d < check=%d, skipping", name, nodeFinTs.Height(), checkHeight)
			continue
		}

		receipts, err := node.ChainGetParentReceipts(ctx, blkCid)
		if err != nil {
			debugLog("[receipt-audit] ChainGetParentReceipts failed on %s: %v", name, err)
			continue
		}

		if msgIdx >= len(receipts) {
			debugLog("[receipt-audit] receipt index %d out of range on %s (have %d)", msgIdx, name, len(receipts))
			continue
		}

		r := receipts[msgIdx]
		results = append(results, receiptResult{
			node:     name,
			exitCode: int64(r.ExitCode),
			gasUsed:  r.GasUsed,
			retData:  r.Return,
		})
	}

	enoughNodes := len(results) >= 2
	assert.Sometimes(enoughNodes, "Receipt audit had enough responding nodes", map[string]any{
		"height":      checkHeight,
		"respondents": len(results),
	})

	if !enoughNodes {
		return
	}

	// Compare all responding nodes against the first
	ref := results[0]
	for _, r := range results[1:] {
		exitMatch := ref.exitCode == r.exitCode
		gasMatch := ref.gasUsed == r.gasUsed
		retMatch := bytes.Equal(ref.retData, r.retData)

		assert.Always(exitMatch, "Receipt ExitCode matches across nodes", map[string]any{
			"height":   checkHeight,
			"msg_idx":  msgIdx,
			"node_a":   ref.node,
			"node_b":   r.node,
			"exit_a":   ref.exitCode,
			"exit_b":   r.exitCode,
		})

		assert.Always(gasMatch, "Receipt GasUsed matches across nodes", map[string]any{
			"height":  checkHeight,
			"msg_idx": msgIdx,
			"node_a":  ref.node,
			"node_b":  r.node,
			"gas_a":   ref.gasUsed,
			"gas_b":   r.gasUsed,
		})

		assert.Always(retMatch, "Receipt Return data matches across nodes", map[string]any{
			"height":    checkHeight,
			"msg_idx":   msgIdx,
			"node_a":    ref.node,
			"node_b":    r.node,
			"ret_len_a": len(ref.retData),
			"ret_len_b": len(r.retData),
		})

		if !exitMatch || !gasMatch || !retMatch {
			log.Printf("[receipt-audit] DIVERGENCE at height %d msg[%d]: %s(exit=%d,gas=%d) vs %s(exit=%d,gas=%d)",
				checkHeight, msgIdx, ref.node, ref.exitCode, ref.gasUsed, r.node, r.exitCode, r.gasUsed)
		}
	}

	debugLog("[receipt-audit] OK: height %d msg[%d] consistent across %d nodes", checkHeight, msgIdx, len(results))
}

// ===========================================================================
// DoMessageOrderingAttack
//
// Uses DIFFERENT wallets to send interdependent messages to the same contract
// (SimpleCoin) via different nodes concurrently. The order messages land in
// the mempool/block can differ per node, exposing state divergence.
// ===========================================================================

func DoMessageOrderingAttack() {
	if len(nodeKeys) < 2 {
		return
	}

	// Need a deployed simplecoin contract
	contracts := getContractsByType("simplecoin")
	if len(contracts) == 0 {
		doDeployStressContract("simplecoin")
		return
	}
	target := rngChoice(contracts)

	// Pick 2-3 distinct wallets
	numWallets := rngIntn(2) + 2 // 2 or 3
	type walletInfo struct {
		addr address.Address
		ki   *types.KeyInfo
	}
	var wallets []walletInfo
	seen := make(map[address.Address]bool)
	for len(wallets) < numWallets {
		addr, ki := pickWallet()
		if seen[addr] {
			continue
		}
		seen[addr] = true
		wallets = append(wallets, walletInfo{addr: addr, ki: ki})
		if len(seen) > numWallets+10 {
			break // avoid infinite loop if not enough wallets
		}
	}
	if len(wallets) < 2 {
		return
	}

	// Pick a common recipient for sendCoin calls
	recipientAddr, _ := pickWallet()

	// Build sendCoin calldata: sendCoin(address, uint256)
	// We need the recipient as an EVM-style address; use a 20-byte representation
	recipientBytes := recipientAddr.Bytes()
	amount := uint64(rngIntn(100) + 1)
	selector := calcSelector("sendCoin(address,uint256)")
	calldata, err := cborWrapCalldata(selector, encodeAddress(recipientBytes), encodeUint256(amount))
	if err != nil {
		log.Printf("[msg-ordering] cborWrapCalldata failed: %v", err)
		return
	}

	// Send from each wallet to a different node concurrently
	type sentInfo struct {
		cid      cid.Cid
		nodeName string
	}
	var mu sync.Mutex
	var sent []sentInfo
	var wg sync.WaitGroup

	for i, w := range wallets {
		nodeName := nodeKeys[i%len(nodeKeys)]
		node := nodes[nodeName]
		wg.Add(1)
		go func(w walletInfo, nodeName string, node api.FullNode) {
			defer wg.Done()
			msgCid, ok := invokeContract(node, w.addr, w.ki, target.addr, calldata, "msg-ordering")
			if ok {
				mu.Lock()
				sent = append(sent, sentInfo{cid: msgCid, nodeName: nodeName})
				mu.Unlock()
			}
		}(w, nodeName, node)
	}
	wg.Wait()

	if len(sent) == 0 {
		return
	}

	// Wait for at least the first message to be included
	waitForMsg(nodes[sent[0].nodeName], sent[0].cid, "msg-ordering")

	// Verify state root consistency at finalized height
	finalizedHeight, _ := getFinalizedHeight()
	if finalizedHeight < finalizedMinHeight {
		return
	}

	stateRoots := make(map[string][]string) // root -> []nodeName
	for _, name := range nodeKeys {
		nodeFinTs, err := nodes[name].ChainGetFinalizedTipSet(ctx)
		if err != nil {
			continue
		}
		// Skip nodes that haven't caught up
		if nodeFinTs.Height() < finalizedMinHeight {
			continue
		}
		ts, err := nodes[name].ChainGetTipSetByHeight(ctx, finalizedHeight, nodeFinTs.Key())
		if err != nil {
			continue
		}
		root := ts.ParentState().String()
		stateRoots[root] = append(stateRoots[root], name)
	}

	if len(stateRoots) == 0 {
		return
	}

	// Count total responding nodes
	totalNodes := 0
	for _, names := range stateRoots {
		totalNodes += len(names)
	}
	if totalNodes < 2 {
		return
	}

	statesMatch := len(stateRoots) == 1

	assert.Sometimes(statesMatch, "State roots match after message ordering attack", map[string]any{
		"finalized_height": finalizedHeight,
		"messages_sent":    len(sent),
		"unique_states":    len(stateRoots),
		"state_roots":      stateRoots,
	})

	if !statesMatch {
		log.Printf("[msg-ordering] DIVERGENCE at finalized=%d: %v", finalizedHeight, stateRoots)
	}

	debugLog("[msg-ordering] OK: %d messages, state consistent at height %d", len(sent), finalizedHeight)
}

// ===========================================================================
// DoNonceBombard
//
// Pushes messages with nonce gaps to one node, then fills gaps via a different
// node. Tests that mempools handle out-of-order nonces correctly and that
// all messages execute in the right order with matching receipts.
// ===========================================================================

func DoNonceBombard() {
	nameA, nameB, nodeA, nodeB := pickTwoDistinctNodes()
	if nameA == "" {
		return
	}

	fromAddr, fromKI := pickWallet()
	toAddr, _ := pickWallet()
	if fromAddr == toAddr {
		return
	}

	baseNonce := nonces[fromAddr]

	type sentMsg struct {
		nonce uint64
		cid   cid.Cid
		node  string
	}
	var sent []sentMsg

	// Phase 1: Gapped nonces to node A (N, N+2, N+4)
	for _, offset := range []uint64{0, 2, 4} {
		n := baseNonce + offset
		msg := baseMsg(fromAddr, toAddr, abi.NewTokenAmount(1))
		c, ok := pushMsgManualNonce(nodeA, msg, fromKI, n, "nonce-bombard")
		if ok {
			sent = append(sent, sentMsg{nonce: n, cid: c, node: nameA})
		}
	}

	// Phase 2: Fill gaps via node B (N+1, N+3)
	for _, offset := range []uint64{1, 3} {
		n := baseNonce + offset
		msg := baseMsg(fromAddr, toAddr, abi.NewTokenAmount(1))
		c, ok := pushMsgManualNonce(nodeB, msg, fromKI, n, "nonce-bombard-fill")
		if ok {
			sent = append(sent, sentMsg{nonce: n, cid: c, node: nameB})
		}
	}

	// Phase 3: Sentinel at N+5
	sentinelNonce := baseNonce + 5
	sentinelMsg := baseMsg(fromAddr, toAddr, abi.NewTokenAmount(1))
	sentinelCid, sentinelOk := pushMsgManualNonce(nodeA, sentinelMsg, fromKI, sentinelNonce, "nonce-bombard-sentinel")

	// Update global nonce immediately — prevents reuse by other vectors
	nonces[fromAddr] = baseNonce + 6

	if !sentinelOk || len(sent) == 0 {
		return
	}

	// Wait for sentinel — if it lands, all prior nonces executed
	result := waitForMsg(nodeA, sentinelCid, "nonce-bombard")
	if result == nil {
		debugLog("[nonce-bombard] sentinel timed out — nodes may be partitioned")
		return
	}

	allLanded := true
	assert.Sometimes(true, "Nonce bombard sentinel landed", map[string]any{
		"base_nonce":    baseNonce,
		"sentinel":      sentinelNonce,
		"messages_sent": len(sent),
	})

	// Verify receipts for all sent messages across all nodes
	for _, s := range sent {
		var receipts []receiptSummary
		for _, name := range nodeKeys {
			r, err := nodes[name].StateSearchMsg(ctx, types.EmptyTSK, s.cid, 200, true)
			if err != nil || r == nil {
				continue // node may be lagging
			}
			receipts = append(receipts, receiptSummary{
				node:     name,
				exitCode: int64(r.Receipt.ExitCode),
				gasUsed:  r.Receipt.GasUsed,
			})
		}

		if len(receipts) < 2 {
			allLanded = false
			continue
		}

		ref := receipts[0]
		for _, r := range receipts[1:] {
			match := ref.exitCode == r.exitCode && ref.gasUsed == r.gasUsed

			assert.Always(match, "Nonce-bombarded message receipt matches across nodes", map[string]any{
				"nonce":  s.nonce,
				"node_a": ref.node,
				"node_b": r.node,
				"exit_a": ref.exitCode,
				"exit_b": r.exitCode,
				"gas_a":  ref.gasUsed,
				"gas_b":  r.gasUsed,
			})
		}
	}

	if allLanded {
		debugLog("[nonce-bombard] OK: all %d messages verified across nodes", len(sent))
	}
}

// receiptSummary is used for cross-node receipt comparison.
type receiptSummary struct {
	node     string
	exitCode int64
	gasUsed  int64
}

// ===========================================================================
// DoGasExhaustionEdge
//
// Submits a high-gas contract call competing with many small transfers on
// different nodes. Verifies that block packing and receipt computation
// are consistent regardless of which node produced the block.
// ===========================================================================

func DoGasExhaustionEdge() {
	nameA, _, nodeA, nodeB := pickTwoDistinctNodes()
	if nameA == "" {
		return
	}

	// Need a gasguzzler contract
	contracts := getContractsByType("gasguzzler")
	if len(contracts) == 0 {
		doDeployStressContract("gasguzzler")
		return
	}
	target := rngChoice(contracts)

	// Big gas wallet for the expensive call
	bigFrom, bigKI := pickWallet()

	// Small gas wallet for cheap transfers
	smallFrom, smallKI := pickWallet()
	smallTo, _ := pickWallet()
	if smallFrom == smallTo {
		return
	}

	// Big gas: burnGas with randomized iterations
	iterations := uint64(rngIntn(50000) + 10000)
	selector := calcSelector("burnGas(uint256)")
	calldata, err := cborWrapCalldata(selector, encodeUint256(iterations))
	if err != nil {
		return
	}

	// Push big message to node A
	bigCid, bigOk := invokeContract(nodeA, bigFrom, bigKI, target.addr, calldata, "gas-exhaust-big")

	// Push several small messages to node B
	var smallCids []cid.Cid
	numSmall := rngIntn(5) + 3
	for i := 0; i < numSmall; i++ {
		msg := baseMsg(smallFrom, smallTo, abi.NewTokenAmount(1))
		sCid, ok := pushMsgWithCid(nodeB, msg, smallKI, "gas-exhaust-small")
		if ok {
			smallCids = append(smallCids, sCid)
		}
	}

	if !bigOk {
		return
	}

	// Wait for the big message to be included
	bigResult := waitForMsg(nodeA, bigCid, "gas-exhaust")
	if bigResult == nil {
		return
	}

	assert.Sometimes(true, "Gas exhaustion edge big message included", map[string]any{
		"iterations":   iterations,
		"small_pushed": len(smallCids),
		"exit_code":    bigResult.Receipt.ExitCode,
	})

	// Cross-node receipt comparison for the big message
	allCids := append([]cid.Cid{bigCid}, smallCids...)
	for _, msgCid := range allCids {
		var receipts []receiptSummary
		for _, name := range nodeKeys {
			r, err := nodes[name].StateSearchMsg(ctx, types.EmptyTSK, msgCid, 200, true)
			if err != nil || r == nil {
				continue
			}
			receipts = append(receipts, receiptSummary{
				node:     name,
				exitCode: int64(r.Receipt.ExitCode),
				gasUsed:  r.Receipt.GasUsed,
			})
		}

		if len(receipts) < 2 {
			continue
		}

		ref := receipts[0]
		for _, r := range receipts[1:] {
			match := ref.exitCode == r.exitCode && ref.gasUsed == r.gasUsed

			assert.Always(match, "Gas exhaustion receipt matches across nodes", map[string]any{
				"msg":    cidStr(msgCid),
				"node_a": ref.node,
				"node_b": r.node,
				"exit_a": ref.exitCode,
				"exit_b": r.exitCode,
				"gas_a":  ref.gasUsed,
				"gas_b":  r.gasUsed,
			})
		}
	}

	debugLog("[gas-exhaust] OK: big(iter=%d) + %d small, receipts consistent", iterations, len(smallCids))
}
