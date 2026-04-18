package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"

	"github.com/antithesishq/antithesis-sdk-go/assert"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/types/ethtypes"
	"github.com/ipfs/go-cid"
)

// ===========================================================================
// Cross-Implementation Vectors (Lotus ↔ Forest)
//
// These vectors actively compare execution results between Lotus and Forest
// at finalized heights. They catch non-deterministic execution bugs (Dec 2020
// chain halt class), HAMT divergence, and FEVM interpreter mismatches.
//
// All comparisons anchor to snapshotMinHeight (shared minimum finalized
// tipset across all nodes) and re-check partitionActive before assertions
// to avoid false positives from n-split aftermath.
// ===========================================================================

// ===========================================================================
// DoCrossImplStateCompute
//
// Calls StateCompute(height, nil, parentTsk) on all nodes at a finalized
// height and compares the resulting root CIDs. Catches the Dec 2020 chain
// halt class: non-deterministic map iteration in actor code producing
// different gas values and thus different state roots.
//
// Unlike DoHeavyCompute (which only checks each node's root against its own
// stored parent state), this cross-compares roots BETWEEN nodes.
// ===========================================================================

func DoCrossImplStateCompute() {
	if len(nodeKeys) < 2 {
		return
	}
	if !allNodesPastEpoch(f3MinEpoch) {
		return
	}
	if partitionActive.Load() {
		return
	}

	snap := getFinalizedSnapshots()
	finalizedHeight, anchorKey := snapshotMinHeight(snap)
	if finalizedHeight < finalizedMinHeight+5 {
		return
	}

	checkHeight := abi.ChainEpoch(rngIntn(int(finalizedHeight-finalizedMinHeight)) + int(finalizedMinHeight))

	type computeResult struct {
		name     string
		nodeImpl string
		root     cid.Cid
		err      error
	}
	var results []computeResult

	for _, name := range nodeKeys {
		node := nodes[name]

		ts, err := node.ChainGetTipSetByHeight(ctx, checkHeight, anchorKey)
		if err != nil {
			debugLog("[cross-compute] ChainGetTipSetByHeight(%d) failed on %s: %v", checkHeight, name, err)
			continue
		}

		parentKey := ts.Parents()
		parentTs, err := node.ChainGetTipSet(ctx, parentKey)
		if err != nil {
			debugLog("[cross-compute] ChainGetTipSet(parent) failed on %s: %v", name, err)
			continue
		}

		st, err := node.StateCompute(ctx, parentTs.Height(), nil, parentKey)
		if err != nil {
			log.Printf("[cross-compute] StateCompute failed on %s at height %d: %v", name, parentTs.Height(), err)
			results = append(results, computeResult{name: name, nodeImpl: nodeType(name), err: err})
			continue
		}

		results = append(results, computeResult{name: name, nodeImpl: nodeType(name), root: st.Root})
	}

	var okResults []computeResult
	for _, r := range results {
		if r.err == nil {
			okResults = append(okResults, r)
		}
	}
	if len(okResults) < 2 {
		return
	}

	implTypes := map[string]bool{}
	rootGroups := map[string][]string{}
	for _, r := range okResults {
		implTypes[r.nodeImpl] = true
		rootGroups[r.root.String()] = append(rootGroups[r.root.String()], r.name)
	}
	crossImpl := implTypes["lotus"] && implTypes["forest"]

	agreed := len(rootGroups) == 1

	details := map[string]any{
		"height":         checkHeight,
		"finalized_at":   finalizedHeight,
		"root_groups":    rootGroups,
		"unique_roots":   len(rootGroups),
		"nodes_checked":  len(okResults),
		"cross_impl":     crossImpl,
	}

	if partitionActive.Load() {
		debugLog("[cross-compute] partition became active mid-check, skipping assertions")
		return
	}

	if checkHeight < finalizedHeight-10 {
		assert.Always(agreed, "Cross-impl StateCompute: all nodes produce same root at deeply finalized height", details)
	} else {
		assert.Sometimes(agreed, "Cross-impl StateCompute: all nodes produce same root near finalized frontier", details)
	}

	if !agreed {
		log.Printf("[cross-compute] STATE ROOT DIVERGENCE at height %d: %v (cross_impl=%v)", checkHeight, rootGroups, crossImpl)
	} else {
		debugLog("[cross-compute] height %d: all %d nodes agree (cross_impl=%v)", checkHeight, len(okResults), crossImpl)
	}

	if crossImpl {
		assert.Always(agreed, "Cross-impl StateCompute: Lotus and Forest produce identical state root", details)
		assert.Sometimes(true, "Cross-impl StateCompute check executed with both implementations", map[string]any{
			"height": checkHeight,
		})
	}
}

// ===========================================================================
// DoDeepActorStateComparison
//
// Picks random actors (system actors, miners, wallets, deployed contracts)
// and calls StateReadState on each node at a finalized height. Compares the
// full serialized state — not just code/nonce/balance like verifyActorConsistency.
//
// Catches HAMT traversal bugs where state roots match but internal data
// structures differ (possible when the HAMT is rebuilt from different insertion
// orders).
// ===========================================================================

func DoDeepActorStateComparison() {
	if len(nodeKeys) < 2 {
		return
	}
	if !allNodesPastEpoch(f3MinEpoch) {
		return
	}
	if partitionActive.Load() {
		return
	}

	finHeight, finTsk := getFinalizedHeight()
	if finHeight < finalizedMinHeight {
		return
	}

	candidates := buildActorCandidates()
	if len(candidates) == 0 {
		return
	}

	numToCheck := rngIntn(3) + 2 // 2-4 actors per invocation
	if numToCheck > len(candidates) {
		numToCheck = len(candidates)
	}

	for i := 0; i < numToCheck; i++ {
		actor := candidates[rngIntn(len(candidates))]
		compareActorState(actor, finHeight, finTsk)
	}
}

func buildActorCandidates() []address.Address {
	var candidates []address.Address

	systemActors := []string{"f00", "f01", "f02", "f03", "f04", "f05", "f06", "f07", "f010", "f099"}
	for _, s := range systemActors {
		a, err := address.NewFromString(s)
		if err == nil {
			candidates = append(candidates, a)
		}
	}

	for _, addr := range addrs {
		candidates = append(candidates, addr)
	}

	contractsMu.Lock()
	for _, c := range deployedContracts {
		candidates = append(candidates, c.addr)
	}
	contractsMu.Unlock()

	return candidates
}

func compareActorState(actor address.Address, finHeight abi.ChainEpoch, finTsk types.TipSetKey) {
	type stateResult struct {
		name     string
		nodeImpl string
		stateJSON []byte
		balance  string
		code     string
		err      error
	}
	var results []stateResult

	for _, name := range nodeKeys {
		node := nodes[name]

		actorState, err := node.StateReadState(ctx, actor, finTsk)
		if err != nil {
			debugLog("[deep-actor] StateReadState(%s) failed on %s: %v", actor, name, err)
			results = append(results, stateResult{name: name, nodeImpl: nodeType(name), err: err})
			continue
		}

		stateBytes, err := json.Marshal(actorState.State)
		if err != nil {
			debugLog("[deep-actor] json.Marshal state failed on %s for %s: %v", name, actor, err)
			results = append(results, stateResult{name: name, nodeImpl: nodeType(name), err: err})
			continue
		}

		results = append(results, stateResult{
			name:      name,
			nodeImpl:  nodeType(name),
			stateJSON: stateBytes,
			balance:   actorState.Balance.String(),
			code:      actorState.Code.String(),
		})
	}

	var okResults []stateResult
	for _, r := range results {
		if r.err == nil {
			okResults = append(okResults, r)
		}
	}
	if len(okResults) < 2 {
		return
	}

	implTypes := map[string]bool{}
	for _, r := range okResults {
		implTypes[r.nodeImpl] = true
	}
	crossImpl := implTypes["lotus"] && implTypes["forest"]

	stateMatch := true
	balanceMatch := true
	codeMatch := true
	for i := 1; i < len(okResults); i++ {
		if !bytes.Equal(okResults[i].stateJSON, okResults[0].stateJSON) {
			stateMatch = false
		}
		if okResults[i].balance != okResults[0].balance {
			balanceMatch = false
		}
		if okResults[i].code != okResults[0].code {
			codeMatch = false
		}
	}

	allMatch := stateMatch && balanceMatch && codeMatch

	stateMap := map[string]string{}
	for _, r := range okResults {
		stateMap[r.name] = fmt.Sprintf("code=%s balance=%s state_len=%d", r.code, r.balance, len(r.stateJSON))
	}

	details := map[string]any{
		"actor":          actor.String(),
		"finalized_at":   finHeight,
		"nodes_checked":  len(okResults),
		"cross_impl":     crossImpl,
		"state_match":    stateMatch,
		"balance_match":  balanceMatch,
		"code_match":     codeMatch,
		"node_states":    stateMap,
	}

	if partitionActive.Load() {
		debugLog("[deep-actor] partition became active mid-check, skipping assertions")
		return
	}

	assert.Always(allMatch, "Deep actor state matches across all nodes at finalized height", details)

	if !allMatch {
		log.Printf("[deep-actor] DIVERGENCE actor=%s at height %d: state=%v balance=%v code=%v (cross_impl=%v)",
			actor, finHeight, stateMatch, balanceMatch, codeMatch, crossImpl)
		for _, r := range okResults {
			log.Printf("[deep-actor]   %s: code=%s balance=%s state=%s", r.name, r.code, r.balance, string(r.stateJSON))
		}
	} else {
		debugLog("[deep-actor] actor=%s at height %d: %d nodes agree (cross_impl=%v)", actor, finHeight, len(okResults), crossImpl)
	}

	if crossImpl {
		assert.Always(allMatch, "Deep actor state: Lotus and Forest agree on full actor state", details)
		assert.Sometimes(true, "Deep actor state cross-impl check executed", map[string]any{
			"actor": actor.String(),
		})
	}
}

// ===========================================================================
// DoCrossImplEthCall
//
// Calls a SimpleCoin getBalance(address) view function via EthCall on all
// nodes at a finalized height. Compares raw return bytes.
//
// Catches FEVM interpreter divergence: Forest reimplements the EVM
// independently, so identical calldata must produce identical return bytes.
// ===========================================================================

func DoCrossImplEthCall() {
	if len(nodeKeys) < 2 {
		return
	}
	if !allNodesPastEpoch(f3MinEpoch) {
		return
	}
	if partitionActive.Load() {
		return
	}

	contracts := getContractsByType("simplecoin")
	if len(contracts) == 0 {
		return
	}
	contract := rngChoice(contracts)

	finHeight, _ := getFinalizedHeight()
	if finHeight < finalizedMinHeight {
		return
	}

	ethAddr, err := ethtypes.EthAddressFromFilecoinAddress(contract.addr)
	if err != nil {
		debugLog("[cross-ethcall] EthAddressFromFilecoinAddress failed: %v", err)
		return
	}

	queryAddr := contract.deployer
	ethQueryAddr, err := ethtypes.EthAddressFromFilecoinAddress(queryAddr)
	if err != nil {
		debugLog("[cross-ethcall] EthAddressFromFilecoinAddress(query) failed: %v", err)
		return
	}

	getBalanceSelector := calcSelector("getBalance(address)")
	calldata := append(getBalanceSelector, encodeAddress(ethQueryAddr[:])...)

	blkParam := ethtypes.NewEthBlockNumberOrHashFromNumber(ethtypes.EthUint64(finHeight))

	type ethResult struct {
		name     string
		nodeImpl string
		result   []byte
		err      error
	}
	var results []ethResult

	for _, name := range nodeKeys {
		node := nodes[name]

		ret, err := node.EthCall(ctx, ethtypes.EthCall{
			To:   &ethAddr,
			Data: ethtypes.EthBytes(calldata),
		}, blkParam)
		if err != nil {
			debugLog("[cross-ethcall] EthCall failed on %s: %v", name, err)
			results = append(results, ethResult{name: name, nodeImpl: nodeType(name), err: err})
			continue
		}

		results = append(results, ethResult{name: name, nodeImpl: nodeType(name), result: []byte(ret)})
	}

	var okResults []ethResult
	for _, r := range results {
		if r.err == nil {
			okResults = append(okResults, r)
		}
	}
	if len(okResults) < 2 {
		return
	}

	implTypes := map[string]bool{}
	resultGroups := map[string][]string{}
	for _, r := range okResults {
		implTypes[r.nodeImpl] = true
		key := hex.EncodeToString(r.result)
		resultGroups[key] = append(resultGroups[key], r.name)
	}
	crossImpl := implTypes["lotus"] && implTypes["forest"]

	agreed := len(resultGroups) == 1

	details := map[string]any{
		"contract":       contract.addr.String(),
		"query_addr":     queryAddr.String(),
		"finalized_at":   finHeight,
		"result_groups":  resultGroups,
		"unique_results": len(resultGroups),
		"nodes_checked":  len(okResults),
		"cross_impl":     crossImpl,
	}

	if partitionActive.Load() {
		debugLog("[cross-ethcall] partition became active mid-check, skipping assertions")
		return
	}

	assert.Always(agreed, "Cross-impl EthCall: all nodes return identical bytes for view function at finalized height", details)

	if !agreed {
		log.Printf("[cross-ethcall] ETHCALL DIVERGENCE contract=%s at height %d: %v (cross_impl=%v)",
			contract.addr, finHeight, resultGroups, crossImpl)
	} else {
		debugLog("[cross-ethcall] contract=%s at height %d: all %d nodes agree (cross_impl=%v)",
			contract.addr, finHeight, len(okResults), crossImpl)
	}

	if crossImpl {
		assert.Always(agreed, "Cross-impl EthCall: Lotus and Forest return identical EVM execution results", details)
		assert.Sometimes(true, "Cross-impl EthCall check executed with both implementations", map[string]any{
			"contract": contract.addr.String(),
		})
	}
}
