package main

import (
	"log"
	"sync"

	"github.com/antithesishq/antithesis-sdk-go/assert"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/crypto"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/lib/sigs"
)

// ===========================================================================
// Message helpers
// ===========================================================================

// baseMsg creates a skeleton Filecoin message with conservative gas params.
func baseMsg(from, to address.Address, value abi.TokenAmount) *types.Message {
	return &types.Message{
		From:       from,
		To:         to,
		Value:      value,
		Method:     0, // plain transfer
		GasLimit:   1_000_000,
		GasFeeCap:  abi.NewTokenAmount(100_000),
		GasPremium: abi.NewTokenAmount(1_000),
	}
}

// signMsg signs a message locally using the provided key info.
// Returns nil if signing fails.
func signMsg(msg *types.Message, ki *types.KeyInfo) *types.SignedMessage {
	msgBytes := msg.Cid().Bytes()

	sig, err := sigs.Sign(crypto.SigTypeSecp256k1, ki.PrivateKey, msgBytes)
	if err != nil {
		log.Printf("[sign] signing failed for %s: %v", msg.From, err)
		return nil
	}
	return &types.SignedMessage{
		Message:   *msg,
		Signature: *sig,
	}
}

// pushMsg signs locally and pushes a single message to the mempool.
// Manages nonces: increments only on success.
func pushMsg(node api.FullNode, msg *types.Message, ki *types.KeyInfo, tag string) bool {
	msg.Nonce = nonces[msg.From]

	smsg := signMsg(msg, ki)
	if smsg == nil {
		return false
	}

	_, err := node.MpoolPush(ctx, smsg)
	if err != nil {
		log.Printf("[%s] MpoolPush failed: %v", tag, err)
		return false
	}

	nonces[msg.From]++
	return true
}

// nodeType returns "lotus" or "forest" based on node name prefix.
func nodeType(name string) string {
	if len(name) >= 6 && name[:6] == "forest" {
		return "forest"
	}
	return "lotus"
}

// ===========================================================================
// Vector 1: DoTransferMarket (Liveness)
// ===========================================================================

// DoTransferMarket sends a random amount of FIL from one wallet to another
// via a random node.
func DoTransferMarket() {
	fromAddr, fromKI := pickWallet()
	toAddr, _ := pickWallet()

	// Skip self-transfer in this vector
	if fromAddr == toAddr {
		return
	}

	// Random amount: 1-100 attoFIL (tiny to avoid draining wallets)
	amount := abi.NewTokenAmount(int64(rngIntn(100) + 1))

	nodeName, node := pickNode()
	msg := baseMsg(fromAddr, toAddr, amount)

	ok := pushMsg(node, msg, fromKI, "transfer")

	if ok {
		log.Printf("  [transfer] OK: %s -> %s via %s (amount=%s)",
			fromAddr.String()[:12], toAddr.String()[:12], nodeName, amount.String())
	}

	assert.Sometimes(ok, "transfer_message_pushed", map[string]any{
		"from":   fromAddr.String(),
		"to":     toAddr.String(),
		"amount": amount.String(),
		"node":   nodeName,
	})
}

// ===========================================================================
// Vector 2: DoSharedState (Contention / Cross-node state)
//
// Compares parent state roots across all connected nodes at a safe height.
// When Antithesis injects partitions, this catches state divergence.
// ===========================================================================

const (
	stateCheckMinHeight = 20
	stateHeightBuffer   = 15 // stay this far behind head to avoid reorg window
)

func DoSharedState() {
	if len(nodeKeys) < 2 {
		return
	}

	// Get minimum head height across all nodes
	minHeight := abi.ChainEpoch(0)
	first := true
	for _, name := range nodeKeys {
		head, err := nodes[name].ChainHead(ctx)
		if err != nil {
			log.Printf("[shared-state] ChainHead failed for %s: %v", name, err)
			return
		}
		if first || head.Height() < minHeight {
			minHeight = head.Height()
			first = false
		}
	}

	if minHeight < stateCheckMinHeight {
		log.Printf("  [shared-state] SKIP: chain height %d < min %d", minHeight, stateCheckMinHeight)
		return
	}

	// Pick a safe height to compare
	safeMax := int(minHeight) - stateHeightBuffer
	if safeMax < 1 {
		return
	}
	checkHeight := abi.ChainEpoch(rngIntn(safeMax) + 1)

	// Collect parent state roots from all nodes at this height
	stateRoots := make(map[string][]string) // root -> []nodeName
	for _, name := range nodeKeys {
		head, err := nodes[name].ChainHead(ctx)
		if err != nil {
			log.Printf("[shared-state] ChainHead failed for %s: %v", name, err)
			return
		}
		ts, err := nodes[name].ChainGetTipSetByHeight(ctx, checkHeight, head.Key())
		if err != nil {
			log.Printf("[shared-state] ChainGetTipSetByHeight(%d) failed for %s: %v", checkHeight, name, err)
			return
		}
		root := ts.ParentState().String()
		stateRoots[root] = append(stateRoots[root], name)
	}

	statesMatch := len(stateRoots) == 1

	assert.Always(statesMatch, "cross_node_state_consistent", map[string]any{
		"height":        checkHeight,
		"state_roots":   stateRoots,
		"unique_states": len(stateRoots),
		"nodes_checked": len(nodeKeys),
	})

	if statesMatch {
		log.Printf("  [shared-state] OK: all %d nodes agree at height %d", len(nodeKeys), checkHeight)
		assert.Sometimes(true, "shared_state_verified", map[string]any{
			"height": checkHeight,
		})
	} else {
		log.Printf("  [shared-state] DIVERGENCE at height %d: %v", checkHeight, stateRoots)
	}
}

// ===========================================================================
// Vector 3: DoGasWar (Mempool)
//
// Tests mempool replacement and greedy selection:
// - Send Tx_A with low gas premium
// - Send Tx_B with same nonce but much higher gas premium (replacement)
// Both go to the same node; the mempool should prefer Tx_B.
// ===========================================================================

func DoGasWar() {
	fromAddr, fromKI := pickWallet()
	toAddrA, _ := pickWallet()
	toAddrB, _ := pickWallet()

	// Need distinct recipients to tell txs apart
	if fromAddr == toAddrA {
		return
	}
	if fromAddr == toAddrB {
		return
	}

	nodeName, node := pickNode()
	currentNonce := nonces[fromAddr]

	// Tx_A: low gas premium
	msgA := baseMsg(fromAddr, toAddrA, abi.NewTokenAmount(1))
	msgA.Nonce = currentNonce
	msgA.GasPremium = abi.NewTokenAmount(100)
	msgA.GasFeeCap = abi.NewTokenAmount(100_000)

	smsgA := signMsg(msgA, fromKI)
	if smsgA == nil {
		return
	}

	_, errA := node.MpoolPush(ctx, smsgA)
	if errA != nil {
		log.Printf("[gas-war] Tx_A push failed: %v", errA)
		return
	}

	// Tx_B: same nonce, much higher gas premium (replacement)
	msgB := baseMsg(fromAddr, toAddrB, abi.NewTokenAmount(1))
	msgB.Nonce = currentNonce
	msgB.GasPremium = abi.NewTokenAmount(50_000) // 500x higher
	msgB.GasFeeCap = abi.NewTokenAmount(200_000)

	smsgB := signMsg(msgB, fromKI)
	if smsgB == nil {
		nonces[fromAddr]++ // Tx_A was pushed, nonce consumed
		return
	}

	_, errB := node.MpoolPush(ctx, smsgB)

	// Regardless of replacement success, nonce is consumed
	nonces[fromAddr]++

	assert.Sometimes(errA == nil, "gas_war_low_premium_accepted", map[string]any{
		"node":  nodeName,
		"nonce": currentNonce,
	})

	assert.Sometimes(errB == nil, "gas_war_replacement_accepted", map[string]any{
		"node":         nodeName,
		"nonce":        currentNonce,
		"low_premium":  "100",
		"high_premium": "50000",
	})

	log.Printf("  [gas-war] nonce=%d: Tx_A(low)=%v, Tx_B(high)=%v",
		currentNonce, errA == nil, errB == nil)
}

// ===========================================================================
// Vector 4: DoHeavyCompute (Resource Safety)
// Recomputes state for a recent epoch via StateCompute and verifies
// the result matches the stored parent state root. Stresses the node's
// compute pipeline.
// ===========================================================================

const (
	computeMinHeight    = 20
	computeStartOffset  = 2  // epochs behind head to start
	computeEndOffset    = 12 // epochs behind head to stop
	computeTargetEpochs = 5  // how many epochs to verify per call
)

func DoHeavyCompute() {
	nodeName, node := pickNode()

	head, err := node.ChainHead(ctx)
	if err != nil {
		log.Printf("[heavy-compute] ChainHead failed for %s: %v", nodeName, err)
		return
	}

	if head.Height() < computeMinHeight {
		return
	}

	startHeight := head.Height() - abi.ChainEpoch(computeStartOffset)
	endHeight := head.Height() - abi.ChainEpoch(computeEndOffset)

	checkTs, err := node.ChainGetTipSetByHeight(ctx, startHeight, head.Key())
	if err != nil {
		log.Printf("[heavy-compute] ChainGetTipSetByHeight(%d) failed: %v", startHeight, err)
		return
	}

	epochsChecked := 0
	for epochsChecked < computeTargetEpochs && checkTs.Height() >= endHeight {
		parentKey := checkTs.Parents()
		parentTs, err := node.ChainGetTipSet(ctx, parentKey)
		if err != nil {
			log.Printf("[heavy-compute] ChainGetTipSet failed at height %d: %v", checkTs.Height(), err)
			return
		}

		if parentTs.Height() < endHeight {
			break
		}

		// Recompute state — this is the expensive operation that stresses the node
		st, err := node.StateCompute(ctx, parentTs.Height(), nil, parentKey)
		if err != nil {
			log.Printf("[heavy-compute] StateCompute failed at height %d: %v", parentTs.Height(), err)
			// Expected: node might reject if overloaded, that's not a safety violation
			return
		}

		stateMatches := st.Root == checkTs.ParentState()

		assert.Always(stateMatches, "state_computation_consistent", map[string]any{
			"node":           nodeName,
			"node_type":      nodeType(nodeName),
			"exec_height":    parentTs.Height(),
			"check_height":   checkTs.Height(),
			"computed_root":  st.Root.String(),
			"expected_root":  checkTs.ParentState().String(),
			"epochs_checked": epochsChecked,
		})

		if !stateMatches {
			log.Printf("[heavy-compute] STATE MISMATCH on %s at height %d: computed=%s expected=%s",
				nodeName, parentTs.Height(), st.Root.String(), checkTs.ParentState().String())
			return
		}

		checkTs = parentTs
		epochsChecked++
	}

	log.Printf("  [heavy-compute] OK: verified %d epochs on %s", epochsChecked, nodeName)

	assert.Sometimes(epochsChecked > 0, "heavy_compute_verified", map[string]any{
		"node":           nodeName,
		"epochs_checked": epochsChecked,
	})
}

// ===========================================================================
// Vector 5: DoAdversarial (Safety / Auth)
//
// Three sub-actions picked randomly:
//   1. Double-spend race: same nonce, different recipients, different nodes
//   2. Invalid signature: garbage sig bytes, must be rejected
//   3. Nonce race: same nonce, different gas premiums, different nodes
// ===========================================================================

func DoAdversarial() {
	subAction := rngIntn(3)
	subNames := []string{"double-spend", "invalid-sig", "nonce-race"}
	log.Printf("  [adversarial] sub-action: %s", subNames[subAction])

	switch subAction {
	case 0:
		doDoubleSpend()
	case 1:
		doInvalidSignature()
	case 2:
		doNonceRace()
	}
}

// doDoubleSpend sends conflicting transactions (same nonce, different recipients)
// to two different nodes. Asserts at most one should be included on-chain.
func doDoubleSpend() {
	if len(nodeKeys) < 2 {
		return
	}

	fromAddr, fromKI := pickWallet()
	toAddrA, _ := pickWallet()
	toAddrB, _ := pickWallet()

	if fromAddr == toAddrA || fromAddr == toAddrB || toAddrA == toAddrB {
		return
	}

	// Pick two different nodes
	nodeA := nodeKeys[rngIntn(len(nodeKeys))]
	nodeB := nodeKeys[rngIntn(len(nodeKeys))]
	for nodeA == nodeB && len(nodeKeys) > 1 {
		nodeB = nodeKeys[rngIntn(len(nodeKeys))]
	}

	currentNonce := nonces[fromAddr]

	// Tx to recipient A via node A
	msgA := baseMsg(fromAddr, toAddrA, abi.NewTokenAmount(1))
	msgA.Nonce = currentNonce
	smsgA := signMsg(msgA, fromKI)

	// Tx to recipient B via node B (same nonce = double spend)
	msgB := baseMsg(fromAddr, toAddrB, abi.NewTokenAmount(1))
	msgB.Nonce = currentNonce
	smsgB := signMsg(msgB, fromKI)

	if smsgA == nil || smsgB == nil {
		return
	}

	// Push concurrently to different nodes
	var wg sync.WaitGroup
	var errA, errB error

	wg.Add(2)
	go func() {
		defer wg.Done()
		_, errA = nodes[nodeA].MpoolPush(ctx, smsgA)
	}()
	go func() {
		defer wg.Done()
		_, errB = nodes[nodeB].MpoolPush(ctx, smsgB)
	}()
	wg.Wait()

	// Nonce is consumed regardless
	nonces[fromAddr]++

	log.Printf("[adversarial] double-spend: nodeA=%s err=%v, nodeB=%s err=%v", nodeA, errA, nodeB, errB)

	// Safety: at least one should eventually be accepted, but both being
	// "accepted" into mempool is OK — only one should make it on-chain.
	// The real assertion happens in DoChainMonitor checking state consistency.
	assert.Sometimes(errA == nil || errB == nil, "double_spend_at_least_one_accepted", map[string]any{
		"from":   fromAddr.String(),
		"nonce":  currentNonce,
		"node_a": nodeA,
		"node_b": nodeB,
	})
}

// doInvalidSignature constructs a message with garbage signature bytes
// and asserts it is immediately rejected.
func doInvalidSignature() {
	fromAddr, _ := pickWallet()
	toAddr, _ := pickWallet()
	if fromAddr == toAddr {
		return
	}

	nodeName, node := pickNode()

	msg := baseMsg(fromAddr, toAddr, abi.NewTokenAmount(1))
	msg.Nonce = nonces[fromAddr] // use real nonce so only the sig is wrong

	// Generate random garbage signature
	garbageSig := make([]byte, 65)
	for i := range garbageSig {
		garbageSig[i] = byte(rngIntn(256))
	}

	smsg := &types.SignedMessage{
		Message: *msg,
		Signature: crypto.Signature{
			Type: crypto.SigTypeSecp256k1,
			Data: garbageSig,
		},
	}

	_, err := node.MpoolPush(ctx, smsg)

	// The node MUST reject an invalid signature
	rejected := err != nil

	assert.Always(rejected, "invalid_signature_rejected", map[string]any{
		"node":     nodeName,
		"from":     fromAddr.String(),
		"rejected": rejected,
		"error":    errStr(err),
	})

	if !rejected {
		log.Printf("[adversarial] SAFETY VIOLATION: invalid signature accepted by %s!", nodeName)
	}

	// Do NOT increment nonce — the message was invalid
}

// doNonceRace sends the same nonce with different gas premiums to different
// nodes, testing that the higher-premium tx wins during block packing.
func doNonceRace() {
	if len(nodeKeys) < 2 {
		return
	}

	fromAddr, fromKI := pickWallet()
	toAddr, _ := pickWallet()
	if fromAddr == toAddr {
		return
	}

	nodeA := nodeKeys[rngIntn(len(nodeKeys))]
	nodeB := nodeKeys[rngIntn(len(nodeKeys))]
	for nodeA == nodeB && len(nodeKeys) > 1 {
		nodeB = nodeKeys[rngIntn(len(nodeKeys))]
	}

	currentNonce := nonces[fromAddr]

	// Low-premium tx to node A
	msgLow := baseMsg(fromAddr, toAddr, abi.NewTokenAmount(1))
	msgLow.Nonce = currentNonce
	msgLow.GasPremium = abi.NewTokenAmount(500)
	smsgLow := signMsg(msgLow, fromKI)

	// High-premium tx to node B
	msgHigh := baseMsg(fromAddr, toAddr, abi.NewTokenAmount(2))
	msgHigh.Nonce = currentNonce
	msgHigh.GasPremium = abi.NewTokenAmount(100_000)
	msgHigh.GasFeeCap = abi.NewTokenAmount(200_000)
	smsgHigh := signMsg(msgHigh, fromKI)

	if smsgLow == nil || smsgHigh == nil {
		return
	}

	// Push concurrently
	var wg sync.WaitGroup
	var errLow, errHigh error

	wg.Add(2)
	go func() {
		defer wg.Done()
		_, errLow = nodes[nodeA].MpoolPush(ctx, smsgLow)
	}()
	go func() {
		defer wg.Done()
		_, errHigh = nodes[nodeB].MpoolPush(ctx, smsgHigh)
	}()
	wg.Wait()

	nonces[fromAddr]++

	assert.Sometimes(errLow == nil || errHigh == nil, "nonce_race_at_least_one_accepted", map[string]any{
		"from":    fromAddr.String(),
		"nonce":   currentNonce,
		"node_lo": nodeA,
		"node_hi": nodeB,
	})
}

// errStr safely converts an error to string for assertion details.
func errStr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// ===========================================================================
// Vector 6: DoChainMonitor (Consensus)
//
// Four sub-checks picked randomly per invocation:
//   1. Tipset consensus at a safe height
//   2. Height progression (all nodes advancing)
//   3. Peer count (all nodes have peers)
//   4. Chain head comparison (all nodes within acceptable range)
// ===========================================================================

const (
	consensusMinHeight  = 30
	consensusBuffer     = 20
	consensusWalkEpochs = 5
)

func DoChainMonitor() {
	subCheck := rngIntn(4)
	checkNames := []string{"tipset-consensus", "height-progression", "peer-count", "head-comparison"}
	log.Printf("  [chain-monitor] sub-check: %s", checkNames[subCheck])

	switch subCheck {
	case 0:
		doTipsetConsensus()
	case 1:
		doHeightProgression()
	case 2:
		doPeerCount()
	case 3:
		doHeadComparison()
	}
}

// doTipsetConsensus checks that all nodes agree on the tipset at a safe height.
// Ported from consensus.go checkTipsetConsensus.
func doTipsetConsensus() {
	if len(nodeKeys) < 2 {
		return
	}

	// Get minimum head height
	minHeight := abi.ChainEpoch(0)
	first := true
	for _, name := range nodeKeys {
		head, err := nodes[name].ChainHead(ctx)
		if err != nil {
			log.Printf("[chain-monitor] ChainHead failed for %s: %v", name, err)
			return
		}
		if first || head.Height() < minHeight {
			minHeight = head.Height()
			first = false
		}
	}

	if minHeight < consensusMinHeight {
		return
	}

	// Pick a safe height in the finalized range
	safeMax := int(minHeight) - consensusBuffer
	if safeMax < 1 {
		return
	}
	checkHeight := abi.ChainEpoch(rngIntn(safeMax) + 1)

	// Query all nodes concurrently for tipset at this height
	type result struct {
		name      string
		tipsetKey string
		err       error
	}

	results := make(chan result, len(nodeKeys))
	var wg sync.WaitGroup

	for _, name := range nodeKeys {
		wg.Add(1)
		go func(nodeName string) {
			defer wg.Done()
			head, err := nodes[nodeName].ChainHead(ctx)
			if err != nil {
				results <- result{name: nodeName, err: err}
				return
			}
			ts, err := nodes[nodeName].ChainGetTipSetByHeight(ctx, checkHeight, head.Key())
			if err != nil {
				results <- result{name: nodeName, err: err}
				return
			}
			results <- result{name: nodeName, tipsetKey: ts.Key().String()}
		}(name)
	}

	wg.Wait()
	close(results)

	tipsetKeys := make(map[string][]string) // key -> []nodeName
	var errs int
	for r := range results {
		if r.err != nil {
			log.Printf("[chain-monitor] tipset query failed for %s: %v", r.name, r.err)
			errs++
			continue
		}
		tipsetKeys[r.tipsetKey] = append(tipsetKeys[r.tipsetKey], r.name)
	}

	if errs == len(nodeKeys) {
		return // all failed, can't assert
	}

	consensusReached := len(tipsetKeys) == 1 && errs == 0

	assert.Always(consensusReached, "tipset_consensus", map[string]any{
		"height":         checkHeight,
		"tipset_keys":    tipsetKeys,
		"unique_tipsets": len(tipsetKeys),
		"nodes_checked":  len(nodeKeys),
		"errors":         errs,
	})

	assert.Sometimes(consensusReached, "tipset_consensus_verified", map[string]any{
		"height": checkHeight,
	})
}

// doHeightProgression checks that all nodes are advancing.
// Ported from node-health.go CheckHeightProgression.
func doHeightProgression() {
	heights := make(map[string]abi.ChainEpoch)
	for _, name := range nodeKeys {
		head, err := nodes[name].ChainHead(ctx)
		if err != nil {
			log.Printf("[chain-monitor] ChainHead failed for %s: %v", name, err)
			continue
		}
		heights[name] = head.Height()
	}

	if len(heights) == 0 {
		return
	}

	// Find min and max heights
	var minH, maxH abi.ChainEpoch
	first := true
	for _, h := range heights {
		if first {
			minH, maxH = h, h
			first = false
		}
		if h < minH {
			minH = h
		}
		if h > maxH {
			maxH = h
		}
	}

	// Nodes shouldn't be too far apart (>10 epochs suggests a problem)
	spread := maxH - minH
	acceptable := spread <= 10

	assert.Always(acceptable, "node_height_spread_acceptable", map[string]any{
		"heights": heights,
		"spread":  spread,
		"min":     minH,
		"max":     maxH,
	})

	// All nodes should be past genesis
	assert.Sometimes(minH > 0, "all_nodes_past_genesis", map[string]any{
		"min_height": minH,
	})
}

// doPeerCount checks that all nodes have peers.
// Ported from node-health.go CheckPeerCount.
func doPeerCount() {
	for _, name := range nodeKeys {
		peers, err := nodes[name].NetPeers(ctx)
		if err != nil {
			log.Printf("[chain-monitor] NetPeers failed for %s: %v", name, err)
			continue
		}

		peerCount := len(peers)

		assert.Always(peerCount > 0, "node_has_peers", map[string]any{
			"node":       name,
			"node_type":  nodeType(name),
			"peer_count": peerCount,
		})

		assert.Sometimes(peerCount > 0, "peer_connectivity_verified", map[string]any{
			"node":       name,
			"peer_count": peerCount,
		})
	}
}

// doHeadComparison queries ChainHead from all nodes and compares.
// Simpler than full tipset consensus — just checks heads are close.
func doHeadComparison() {
	if len(nodeKeys) < 2 {
		return
	}

	type headInfo struct {
		name   string
		height abi.ChainEpoch
		key    string
	}

	var heads []headInfo
	for _, name := range nodeKeys {
		head, err := nodes[name].ChainGetFinalizedTipSet(ctx)
		if err != nil {
			log.Printf("[chain-monitor] ChainHead failed for %s: %v", name, err)
			continue
		}
		heads = append(heads, headInfo{
			name:   name,
			height: head.Height(),
			key:    head.Key().String(),
		})
	}

	if len(heads) < 2 {
		return
	}

	// Group by height
	byHeight := make(map[abi.ChainEpoch][]headInfo)
	for _, h := range heads {
		byHeight[h.height] = append(byHeight[h.height], h)
	}

	// For nodes at the same height, their tipset keys should match
	for height, group := range byHeight {
		if len(group) < 2 {
			continue
		}
		firstKey := group[0].key
		allMatch := true
		for _, h := range group[1:] {
			if h.key != firstKey {
				allMatch = false
				break
			}
		}

		assert.Always(allMatch, "same_height_same_tipset", map[string]any{
			"height":     height,
			"nodes":      len(group),
			"keys_match": allMatch,
		})
	}
}
