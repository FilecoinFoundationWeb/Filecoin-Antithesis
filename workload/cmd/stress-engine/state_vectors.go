package main

import (
	"bytes"
	"log"

	"github.com/antithesishq/antithesis-sdk-go/assert"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/builtin/v15/eam"
	"github.com/filecoin-project/lotus/chain/types"
)

// ===========================================================================
// State Tree Consistency Vectors
//
// These vectors stress actor creation/destruction and verify that the state
// tree (HAMT) remains consistent across all nodes. They target the kind of
// bugs where implementations disagree on actor existence or state after
// complex lifecycle operations.
//
// All verification uses verifyActorConsistency (helpers.go) which queries
// at finalized tipsets and skips lagging/disconnected nodes.
// ===========================================================================

// ===========================================================================
// DoActorMigrationStress
//
// Burst-creates multiple actors (selfdestruct contracts), destroys some,
// then verifies the state tree is consistent across all nodes. Stresses
// HAMT insert/delete paths and actor registry bookkeeping.
// ===========================================================================

func DoActorMigrationStress() {
	if len(nodeKeys) < 2 {
		return
	}

	numDeploys := rngIntn(3) + 3 // 3-5 deploys

	bytecode := contractBytecodes["selfdestruct"]
	if bytecode == nil {
		return
	}

	type deployed struct {
		addr     address.Address
		deployer address.Address
		ki       *types.KeyInfo
	}
	var contracts []deployed

	// Phase 1: Burst deploy selfdestruct contracts
	for range numDeploys {
		fromAddr, fromKI := pickWallet()
		_, node := pickNode()

		msgCid, ok := deployContract(node, fromAddr, fromKI, bytecode, "migration-deploy")
		if !ok {
			continue
		}

		result := waitForMsg(node, msgCid, "migration-deploy")
		if result == nil || !result.Receipt.ExitCode.IsSuccess() {
			continue
		}

		var ret eam.CreateExternalReturn
		if err := ret.UnmarshalCBOR(bytes.NewReader(result.Receipt.Return)); err != nil {
			log.Printf("[migration] decode CreateReturn failed: %v", err)
			continue
		}
		idAddr, err := address.NewIDAddress(ret.ActorID)
		if err != nil {
			continue
		}

		contracts = append(contracts, deployed{addr: idAddr, deployer: fromAddr, ki: fromKI})
		debugLog("[migration] deployed contract %d/%d at %s", len(contracts), numDeploys, idAddr)
	}

	someDeployed := len(contracts) > 0
	assert.Sometimes(someDeployed, "Actor migration deployed at least one contract", map[string]any{
		"attempted": numDeploys,
		"deployed":  len(contracts),
	})

	if !someDeployed {
		return
	}

	// Phase 2: Destroy a random subset
	numDestroy := rngIntn(len(contracts)/2) + 1
	if numDestroy > len(contracts) {
		numDestroy = len(contracts)
	}

	destroyCalldata, err := cborWrapCalldata(calcSelector("destroy()"))
	if err != nil {
		return
	}

	destroyedCount := 0
	for i := 0; i < numDestroy; i++ {
		c := contracts[i]
		_, node := pickNode()

		destroyCid, ok := invokeContract(node, c.deployer, c.ki, c.addr, destroyCalldata, "migration-destroy")
		if !ok {
			continue
		}

		dr := waitForMsg(node, destroyCid, "migration-destroy")
		if dr != nil && dr.Receipt.ExitCode.IsSuccess() {
			destroyedCount++
			debugLog("[migration] destroyed contract at %s", c.addr)
		}
	}

	assert.Sometimes(destroyedCount > 0, "Actor migration destroyed at least one contract", map[string]any{
		"attempted": numDestroy,
		"destroyed": destroyedCount,
	})

	// Phase 3: Verify state tree consistency across all nodes
	for _, c := range contracts {
		verifyActorConsistency(c.addr, "post-migration")
	}

	debugLog("[migration] OK: deployed=%d destroyed=%d, state verified", len(contracts), destroyedCount)
}

// ===========================================================================
// DoActorLifecycleStress
//
// Full actor lifecycle: deploy → fund → invoke → self-destruct → interact
// with the dead address. Verifies cross-node state consistency at every step.
//
// This is more thorough than DoSelfDestructCycle which only does deploy →
// destroy → verify. Here we exercise intermediate state transitions and
// attempt post-mortem interactions.
// ===========================================================================

func DoActorLifecycleStress() {
	if len(nodeKeys) < 2 {
		return
	}

	fromAddr, fromKI := pickWallet()
	_, primaryNode := pickNode()

	bytecode := contractBytecodes["selfdestruct"]
	if bytecode == nil {
		return
	}

	// Step 1: Deploy
	deployCid, ok := deployContract(primaryNode, fromAddr, fromKI, bytecode, "lifecycle-deploy")
	if !ok {
		return
	}
	deployResult := waitForMsg(primaryNode, deployCid, "lifecycle-deploy")
	if deployResult == nil || !deployResult.Receipt.ExitCode.IsSuccess() {
		return
	}

	var ret eam.CreateExternalReturn
	if err := ret.UnmarshalCBOR(bytes.NewReader(deployResult.Receipt.Return)); err != nil {
		log.Printf("[lifecycle] decode CreateReturn failed: %v", err)
		return
	}
	contractAddr, err := address.NewIDAddress(ret.ActorID)
	if err != nil {
		return
	}

	debugLog("[lifecycle] deployed at %s", contractAddr)
	verifyActorConsistency(contractAddr, "post-deploy")

	// Step 2: Fund — send FIL to the contract
	fundMsg := baseMsg(fromAddr, contractAddr, abi.NewTokenAmount(1000))
	fundCid, ok := pushMsgWithCid(primaryNode, fundMsg, fromKI, "lifecycle-fund")
	if !ok {
		return
	}
	fundResult := waitForMsg(primaryNode, fundCid, "lifecycle-fund")
	if fundResult == nil {
		return
	}

	debugLog("[lifecycle] funded %s", contractAddr)
	verifyActorConsistency(contractAddr, "post-fund")

	// Step 3: Invoke — plain transfer to exercise the actor
	invokeMsg := baseMsg(fromAddr, contractAddr, abi.NewTokenAmount(1))
	invokeCid, ok := pushMsgWithCid(primaryNode, invokeMsg, fromKI, "lifecycle-invoke")
	if !ok {
		return
	}
	invokeResult := waitForMsg(primaryNode, invokeCid, "lifecycle-invoke")
	if invokeResult == nil {
		return
	}

	debugLog("[lifecycle] invoked %s", contractAddr)
	verifyActorConsistency(contractAddr, "post-invoke")

	// Step 4: Self-destruct
	destroyCalldata, err := cborWrapCalldata(calcSelector("destroy()"))
	if err != nil {
		return
	}

	destroyCid, ok := invokeContract(primaryNode, fromAddr, fromKI, contractAddr, destroyCalldata, "lifecycle-destroy")
	if !ok {
		return
	}
	destroyResult := waitForMsg(primaryNode, destroyCid, "lifecycle-destroy")
	if destroyResult == nil || !destroyResult.Receipt.ExitCode.IsSuccess() {
		return
	}

	debugLog("[lifecycle] destroyed %s", contractAddr)
	verifyActorConsistency(contractAddr, "post-destroy")

	// Step 5: Interact with dead address — send FIL to the now-destroyed contract
	deadMsg := baseMsg(fromAddr, contractAddr, abi.NewTokenAmount(1))
	deadCid, ok := pushMsgWithCid(primaryNode, deadMsg, fromKI, "lifecycle-dead")
	if ok {
		deadResult := waitForMsg(primaryNode, deadCid, "lifecycle-dead")
		if deadResult != nil {
			debugLog("[lifecycle] post-mortem msg to %s: exit=%d", contractAddr, deadResult.Receipt.ExitCode)
			verifyActorConsistency(contractAddr, "post-dead-interact")
		}
	}

	assert.Sometimes(true, "Actor lifecycle completed all 5 phases", map[string]any{
		"contract": contractAddr.String(),
	})

	debugLog("[lifecycle] OK: full lifecycle completed for %s", contractAddr)
}
