package main

import (
	"bytes"
	"context"
	"log"
	"sync"
	"time"

	"github.com/antithesishq/antithesis-sdk-go/assert"
	"github.com/antithesishq/antithesis-sdk-go/random"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	builtintypes "github.com/filecoin-project/go-state-types/builtin"
	"github.com/filecoin-project/go-state-types/builtin/v15/eam"
	"github.com/filecoin-project/lotus/chain/types"
)

const stateWaitTimeout = 2 * time.Minute

// ===========================================================================
// Vector 7: DoDeployContracts (FVM Stress — Contract Deployment)
//
// Deploys EVM contracts via EAM.CreateExternal to stress the Init actor,
// state tree growth, and FVM constructor execution. On subsequent calls,
// checks pending deploys for confirmation and registers deployed contracts.
// ===========================================================================

const maxPendingDeploys = 50

func DoDeployContracts() {
	// Phase 1: Check pending deploys for confirmation
	resolvePendingDeploys()

	// Phase 2: Deploy a new contract
	if len(contractTypes) == 0 {
		return
	}

	ctype := rngChoice(contractTypes)
	bytecode := contractBytecodes[ctype]
	fromAddr, fromKI := pickWallet()
	nodeName, node := pickNode()

	msgCid, ok := deployContract(node, fromAddr, fromKI, bytecode, "deploy-"+ctype)
	if !ok {
		log.Printf("[deploy] failed to deploy %s via %s", ctype, nodeName)
		return
	}

	// Get current head height for tracking
	head, err := node.ChainHead(ctx)
	epoch := abi.ChainEpoch(0)
	if err == nil {
		epoch = head.Height()
	}

	pendingMu.Lock()
	if len(pendingDeploys) < maxPendingDeploys {
		pendingDeploys = append(pendingDeploys, pendingDeploy{
			msgCid:   msgCid,
			ctype:    ctype,
			deployer: fromAddr,
			deployKI: fromKI,
			epoch:    epoch,
		})
	}
	pendingMu.Unlock()

	debugLog("  [deploy] submitted %s deploy via %s (cid=%s)", ctype, nodeName, msgCid.String()[:16])

	assert.Sometimes(true, "contract_deploy_submitted", map[string]any{
		"type": ctype,
		"node": nodeName,
	})
}

func resolvePendingDeploys() {
	pendingMu.Lock()
	pending := pendingDeploys
	pendingDeploys = nil
	pendingMu.Unlock()

	if len(pending) == 0 {
		return
	}

	node := nodes[nodeKeys[0]]

	var remaining []pendingDeploy
	for _, pd := range pending {
		result, err := node.StateSearchMsg(ctx, types.EmptyTSK, pd.msgCid, 100, true)
		if err != nil || result == nil {
			// Not found yet — keep waiting
			remaining = append(remaining, pd)
			continue
		}

		if result.Receipt.ExitCode.IsSuccess() {
			// Decode the CreateExternalReturn to get the contract address
			var ret eam.CreateExternalReturn
			if err := ret.UnmarshalCBOR(bytes.NewReader(result.Receipt.Return)); err != nil {
				log.Printf("[deploy] failed to decode CreateReturn: %v", err)
				continue
			}
			idAddr, err := address.NewIDAddress(ret.ActorID)
			if err != nil {
				log.Printf("[deploy] failed to create ID address: %v", err)
				continue
			}

			contractsMu.Lock()
			deployedContracts = append(deployedContracts, deployedContract{
				addr:     idAddr,
				ctype:    pd.ctype,
				deployer: pd.deployer,
				deployKI: pd.deployKI,
			})
			contractsMu.Unlock()

			debugLog("  [deploy] confirmed %s at %s (actor=%d)", pd.ctype, idAddr, ret.ActorID)
			assert.Sometimes(true, "contract_deployed", map[string]any{
				"type":     pd.ctype,
				"actor_id": ret.ActorID,
			})
		} else {
			log.Printf("  [deploy] %s failed with exit code %d", pd.ctype, result.Receipt.ExitCode)
		}
	}

	if len(remaining) > 0 {
		pendingMu.Lock()
		pendingDeploys = append(remaining, pendingDeploys...)
		pendingMu.Unlock()
	}
}

// ===========================================================================
// Vector 8: DoContractCall (FVM Stress — Contract Invocation)
//
// Invokes deployed contracts with stress patterns:
// - Deep recursion (Recursive.recursiveCall)
// - Delegatecall recursion (RecursiveDelegatecall.recursiveCall)
// - SimpleCoin token transfers
// - External recursive calls (StackRecCallExp.exec1)
// ===========================================================================

func DoContractCall() {
	contractsMu.Lock()
	numContracts := len(deployedContracts)
	contractsMu.Unlock()

	if numContracts == 0 {
		log.Printf("  [contract-call] SKIP: no deployed contracts yet")
		return
	}

	subAction := rngIntn(4)
	subNames := []string{"deep-recursion", "delegatecall-recursion", "simplecoin-transfer", "external-recursion"}
	debugLog("  [contract-call] sub-action: %s", subNames[subAction])

	switch subAction {
	case 0:
		doDeepRecursion()
	case 1:
		doDelegatecallRecursion()
	case 2:
		doSimpleCoinTransfer()
	case 3:
		doExternalRecursion()
	}
}

func doDeepRecursion() {
	contracts := getContractsByType("recursive")
	if len(contracts) == 0 {
		return
	}
	c := rngChoice(contracts)
	nodeName, node := pickNode()

	// Random recursion depth: 1-100
	depth := uint64(rngIntn(100) + 1)

	// recursiveCall(uint256)
	calldata, err := cborWrapCalldata(calcSelector("recursiveCall(uint256)"), encodeUint256(depth))
	if err != nil {
		log.Printf("[contract-call] cborWrap failed: %v", err)
		return
	}

	msgCid, ok := invokeContract(node, c.deployer, c.deployKI, c.addr, calldata, "recursive-call")

	debugLog("  [contract-call] recursive depth=%d via %s ok=%v cid=%s",
		depth, nodeName, ok, cidStr(msgCid))

	assert.Sometimes(ok, "contract_call_submitted", map[string]any{
		"type":  "recursive",
		"depth": depth,
		"node":  nodeName,
	})
}

func doDelegatecallRecursion() {
	contracts := getContractsByType("delegatecall")
	if len(contracts) == 0 {
		return
	}
	c := rngChoice(contracts)
	nodeName, node := pickNode()

	// Random recursion depth: 1-50 (delegatecall is more expensive)
	depth := uint64(rngIntn(50) + 1)

	// recursiveCall(uint256)
	calldata, err := cborWrapCalldata(calcSelector("recursiveCall(uint256)"), encodeUint256(depth))
	if err != nil {
		return
	}

	msgCid, ok := invokeContract(node, c.deployer, c.deployKI, c.addr, calldata, "delegatecall-call")

	debugLog("  [contract-call] delegatecall depth=%d via %s ok=%v cid=%s",
		depth, nodeName, ok, cidStr(msgCid))

	assert.Sometimes(ok, "delegatecall_submitted", map[string]any{
		"type":  "delegatecall",
		"depth": depth,
		"node":  nodeName,
	})
}

func doSimpleCoinTransfer() {
	contracts := getContractsByType("simplecoin")
	if len(contracts) == 0 {
		return
	}
	c := rngChoice(contracts)
	nodeName, node := pickNode()

	// Pick a random recipient address — use raw 20-byte address for EVM
	toAddr, _ := pickWallet()
	toBytes := toAddr.Payload()

	// Random amount: 1-100 tokens
	amount := uint64(rngIntn(100) + 1)

	// sendCoin(address,uint256)
	calldata, err := cborWrapCalldata(
		calcSelector("sendCoin(address,uint256)"),
		encodeAddress(toBytes),
		encodeUint256(amount),
	)
	if err != nil {
		return
	}

	msgCid, ok := invokeContract(node, c.deployer, c.deployKI, c.addr, calldata, "simplecoin-send")

	debugLog("  [contract-call] simplecoin send amount=%d via %s ok=%v cid=%s",
		amount, nodeName, ok, cidStr(msgCid))

	assert.Sometimes(ok, "simplecoin_transfer_submitted", map[string]any{
		"amount": amount,
		"node":   nodeName,
	})
}

func doExternalRecursion() {
	contracts := getContractsByType("extrecursive")
	if len(contracts) == 0 {
		return
	}
	c := rngChoice(contracts)
	nodeName, node := pickNode()

	// Random recursion depth: 1-30 (external calls are very expensive)
	depth := uint64(rngIntn(30) + 1)

	// exec1(uint256)
	calldata, err := cborWrapCalldata(calcSelector("exec1(uint256)"), encodeUint256(depth))
	if err != nil {
		return
	}

	msgCid, ok := invokeContract(node, c.deployer, c.deployKI, c.addr, calldata, "ext-recursive-call")

	debugLog("  [contract-call] external recursion depth=%d via %s ok=%v cid=%s",
		depth, nodeName, ok, cidStr(msgCid))

	assert.Sometimes(ok, "external_recursion_submitted", map[string]any{
		"type":  "extrecursive",
		"depth": depth,
		"node":  nodeName,
	})
}

// ===========================================================================
// Vector 9: DoSelfDestructCycle (Actor Lifecycle Stress)
//
// Deploys a SelfDestruct contract, then calls destroy() to kill it.
// Verifies actor state is consistent across nodes after destruction.
// ===========================================================================

func DoSelfDestructCycle() {
	fromAddr, fromKI := pickWallet()
	nodeName, node := pickNode()

	// Deploy the SelfDestruct contract
	bytecode := contractBytecodes["selfdestruct"]
	if bytecode == nil {
		return
	}

	msgCid, ok := deployContract(node, fromAddr, fromKI, bytecode, "selfdestruct-deploy")
	if !ok {
		return
	}

	// Wait for deployment confirmation (with timeout to avoid blocking the main loop)
	waitCtx, waitCancel := context.WithTimeout(ctx, stateWaitTimeout)
	result, err := node.StateWaitMsg(waitCtx, msgCid, 1, 200, false)
	waitCancel()
	if err != nil {
		log.Printf("[selfdestruct] StateWaitMsg failed: %v", err)
		return
	}
	if !result.Receipt.ExitCode.IsSuccess() {
		log.Printf("[selfdestruct] deploy failed with exit code %d", result.Receipt.ExitCode)
		return
	}

	// Decode contract address
	var ret eam.CreateExternalReturn
	if err := ret.UnmarshalCBOR(bytes.NewReader(result.Receipt.Return)); err != nil {
		log.Printf("[selfdestruct] decode CreateReturn failed: %v", err)
		return
	}
	contractAddr, err := address.NewIDAddress(ret.ActorID)
	if err != nil {
		return
	}

	debugLog("  [selfdestruct] deployed at %s, now destroying...", contractAddr)

	// Call destroy() on the contract
	calldata, err := cborWrapCalldata(calcSelector("destroy()"))
	if err != nil {
		return
	}

	destroyCid, ok := invokeContract(node, fromAddr, fromKI, contractAddr, calldata, "selfdestruct-destroy")
	if !ok {
		return
	}

	// Wait for destroy confirmation (with timeout to avoid blocking the main loop)
	waitCtx2, waitCancel2 := context.WithTimeout(ctx, stateWaitTimeout)
	destroyResult, err := node.StateWaitMsg(waitCtx2, destroyCid, 1, 200, false)
	waitCancel2()
	if err != nil {
		log.Printf("[selfdestruct] destroy StateWaitMsg failed: %v", err)
		return
	}

	destroyed := destroyResult.Receipt.ExitCode.IsSuccess()

	assert.Sometimes(destroyed, "selfdestruct_executed", map[string]any{
		"contract": contractAddr.String(),
		"node":     nodeName,
	})

	if !destroyed {
		log.Printf("[selfdestruct] destroy failed with exit code %d", destroyResult.Receipt.ExitCode)
		return
	}

	debugLog("  [selfdestruct] destroyed %s, verifying across nodes...", contractAddr)

	// Verify actor state across nodes — both should agree on the contract state.
	// Use the tipset from the confirmed destroy receipt (not ChainHead) to avoid
	// race conditions where other nodes haven't synced the latest head yet.
	if len(nodeKeys) >= 2 {
		verifyTsk := destroyResult.TipSet

		var results []string
		var nodeResults []string // only nodes that successfully responded
		for _, name := range nodeKeys {
			actor, err := nodes[name].StateGetActor(ctx, contractAddr, verifyTsk)
			if err != nil {
				log.Printf("[selfdestruct] StateGetActor failed for %s: %v", name, err)
				results = append(results, name+":error")
			} else if actor == nil {
				results = append(results, "nil")
				nodeResults = append(nodeResults, "nil")
			} else {
				results = append(results, actor.Code.String())
				nodeResults = append(nodeResults, actor.Code.String())
			}
		}

		// Only assert divergence across nodes that successfully responded.
		// An RPC error from a node is a connectivity issue, not a state disagreement.
		allSame := true
		for i := 1; i < len(nodeResults); i++ {
			if nodeResults[i] != nodeResults[0] {
				allSame = false
				break
			}
		}

		assert.Always(allSame, "selfdestruct_state_correct", map[string]any{
			"contract": contractAddr.String(),
			"results":  results,
		})

		if !allSame {
			log.Printf("[selfdestruct] STATE DIVERGENCE after destroy: %v", results)
		}
	}
}

// ===========================================================================
// Vector 10: DoConflictingContractCalls (Contract State Race)
//
// Sends conflicting SimpleCoin.sendCoin() calls with the same nonce to
// different nodes. Only one should succeed on-chain. Both nodes must agree.
// ===========================================================================

func DoConflictingContractCalls() {
	if len(nodeKeys) < 2 {
		return
	}

	contracts := getContractsByType("simplecoin")
	if len(contracts) == 0 {
		return
	}
	c := rngChoice(contracts)

	// Pick two different recipients
	toAddrA, _ := pickWallet()
	toAddrB, _ := pickWallet()
	if toAddrA == toAddrB {
		return
	}

	// Pick two different nodes
	nodeA := nodeKeys[rngIntn(len(nodeKeys))]
	nodeB := nodeKeys[rngIntn(len(nodeKeys))]
	for nodeA == nodeB && len(nodeKeys) > 1 {
		nodeB = nodeKeys[rngIntn(len(nodeKeys))]
	}

	currentNonce := nonces[c.deployer]

	// Large amount to ensure conflict (only 10000 tokens in contract)
	amount := uint64(8000)

	// Build calldata for sendCoin(address,uint256) to two different recipients
	calldataA, err := cborWrapCalldata(
		calcSelector("sendCoin(address,uint256)"),
		encodeAddress(toAddrA.Payload()),
		encodeUint256(amount),
	)
	if err != nil {
		return
	}

	calldataB, err := cborWrapCalldata(
		calcSelector("sendCoin(address,uint256)"),
		encodeAddress(toAddrB.Payload()),
		encodeUint256(amount),
	)
	if err != nil {
		return
	}

	// Build messages with same nonce
	msgA := &types.Message{
		From:   c.deployer,
		To:     c.addr,
		Value:  abi.NewTokenAmount(0),
		Method: builtintypes.MethodsEVM.InvokeContract,
		Params: calldataA,
		Nonce:  currentNonce,
	}

	msgB := &types.Message{
		From:   c.deployer,
		To:     c.addr,
		Value:  abi.NewTokenAmount(0),
		Method: builtintypes.MethodsEVM.InvokeContract,
		Params: calldataB,
		Nonce:  currentNonce,
	}

	// Estimate gas for both
	gasA, err := nodes[nodeA].GasEstimateMessageGas(ctx, msgA, nil, types.EmptyTSK)
	if err != nil {
		msgA.GasLimit = 10_000_000_000
		msgA.GasFeeCap = abi.NewTokenAmount(150_000)
		msgA.GasPremium = abi.NewTokenAmount(1_000)
	} else {
		msgA.GasLimit = gasA.GasLimit
		msgA.GasFeeCap = gasA.GasFeeCap
		msgA.GasPremium = gasA.GasPremium
	}

	gasB, err := nodes[nodeB].GasEstimateMessageGas(ctx, msgB, nil, types.EmptyTSK)
	if err != nil {
		msgB.GasLimit = 10_000_000_000
		msgB.GasFeeCap = abi.NewTokenAmount(150_000)
		msgB.GasPremium = abi.NewTokenAmount(1_000)
	} else {
		msgB.GasLimit = gasB.GasLimit
		msgB.GasFeeCap = gasB.GasFeeCap
		msgB.GasPremium = gasB.GasPremium
	}

	smsgA := signMsg(msgA, c.deployKI)
	smsgB := signMsg(msgB, c.deployKI)
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

	nonces[c.deployer]++

	debugLog("[contract-race] conflicting sendCoin: nodeA=%s err=%v, nodeB=%s err=%v",
		nodeA, errA, nodeB, errB)

	assert.Sometimes(errA == nil || errB == nil, "conflicting_contract_call_accepted", map[string]any{
		"contract": c.addr.String(),
		"nonce":    currentNonce,
		"node_a":   nodeA,
		"node_b":   nodeB,
	})
}

// ===========================================================================
// Resource Stress Vectors
//
// Deploy and invoke contracts that target specific node resource limits:
// block gas, event/receipt storage, EVM memory allocation, and state trie
// (HAMT) growth.
//
// Goal: trigger OOM, excessive disk I/O, bloom filter blowup, and state
// trie degradation that could cause node crashes or consensus splits.
// ===========================================================================

// DoGasGuzzler calls burnGas(iterations) — tight keccak256 loop to max
// out block gas consumption and stress the compute pipeline.
func DoGasGuzzler() {
	contracts := getContractsByType("gasguzzler")
	if len(contracts) == 0 {
		doDeployStressContract("gasguzzler")
		return
	}

	c := rngChoice(contracts)
	nodeName, node := pickNode()

	// Random iterations: 500-10000 (each iteration ~36 gas for keccak256)
	iterations := uint64(rngIntn(9500) + 500)

	calldata, err := cborWrapCalldata(calcSelector("burnGas(uint256)"), encodeUint256(iterations))
	if err != nil {
		log.Printf("[gas-guzzler] cborWrap failed: %v", err)
		return
	}

	msgCid, ok := invokeContract(node, c.deployer, c.deployKI, c.addr, calldata, "gas-guzzler")

	debugLog("  [gas-guzzler] iterations=%d via %s ok=%v cid=%s",
		iterations, nodeName, ok, cidStr(msgCid))

	assert.Sometimes(ok, "gas_guzzler_submitted", map[string]any{
		"iterations": iterations,
		"node":       nodeName,
	})
}

// DoLogBlaster calls blastLogs(count) — emits massive numbers of events
// to stress receipt storage, bloom filter computation, and event indexing.
func DoLogBlaster() {
	contracts := getContractsByType("logblaster")
	if len(contracts) == 0 {
		doDeployStressContract("logblaster")
		return
	}

	c := rngChoice(contracts)
	nodeName, node := pickNode()

	// Random event count: 50-500 (each LOG2 costs ~1125 gas + data)
	count := uint64(rngIntn(450) + 50)

	calldata, err := cborWrapCalldata(calcSelector("blastLogs(uint256)"), encodeUint256(count))
	if err != nil {
		log.Printf("[log-blaster] cborWrap failed: %v", err)
		return
	}

	msgCid, ok := invokeContract(node, c.deployer, c.deployKI, c.addr, calldata, "log-blaster")

	debugLog("  [log-blaster] count=%d via %s ok=%v cid=%s",
		count, nodeName, ok, cidStr(msgCid))

	assert.Sometimes(ok, "log_blaster_submitted", map[string]any{
		"count": count,
		"node":  nodeName,
	})
}

// DoMemoryBomb calls expandMemory(words) — allocates EVM memory with
// quadratic cost growth. Targets node-side allocator and FVM memory accounting.
func DoMemoryBomb() {
	contracts := getContractsByType("memorybomb")
	if len(contracts) == 0 {
		doDeployStressContract("memorybomb")
		return
	}

	c := rngChoice(contracts)
	nodeName, node := pickNode()

	// Random words: 100-5000 (memory cost grows quadratically)
	words := uint64(rngIntn(4900) + 100)

	calldata, err := cborWrapCalldata(calcSelector("expandMemory(uint256)"), encodeUint256(words))
	if err != nil {
		log.Printf("[memory-bomb] cborWrap failed: %v", err)
		return
	}

	msgCid, ok := invokeContract(node, c.deployer, c.deployKI, c.addr, calldata, "memory-bomb")

	debugLog("  [memory-bomb] words=%d via %s ok=%v cid=%s",
		words, nodeName, ok, cidStr(msgCid))

	assert.Sometimes(ok, "memory_bomb_submitted", map[string]any{
		"words": words,
		"node":  nodeName,
	})
}

// DoStorageSpam calls spamSlots(count, seed) — writes to many unique storage
// slots per call. Each new SSTORE costs 20,000 gas. Stresses the HAMT
// (state trie), SplitStore compaction, and snapshot size.
func DoStorageSpam() {
	contracts := getContractsByType("storagespam")
	if len(contracts) == 0 {
		doDeployStressContract("storagespam")
		return
	}

	c := rngChoice(contracts)
	nodeName, node := pickNode()

	// Random slot count: 10-200 (each SSTORE to new slot = 20k gas)
	count := uint64(rngIntn(190) + 10)
	// Random seed so each call hits different slots
	seed := random.GetRandom()

	calldata, err := cborWrapCalldata(
		calcSelector("spamSlots(uint256,uint256)"),
		encodeUint256(count),
		encodeUint256(seed),
	)
	if err != nil {
		log.Printf("[storage-spam] cborWrap failed: %v", err)
		return
	}

	msgCid, ok := invokeContract(node, c.deployer, c.deployKI, c.addr, calldata, "storage-spam")

	debugLog("  [storage-spam] count=%d seed=%d via %s ok=%v cid=%s",
		count, seed, nodeName, ok, cidStr(msgCid))

	assert.Sometimes(ok, "storage_spam_submitted", map[string]any{
		"count": count,
		"node":  nodeName,
	})
}
