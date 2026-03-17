package main

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	builtintypes "github.com/filecoin-project/go-state-types/builtin"
	miner15 "github.com/filecoin-project/go-state-types/builtin/v15/miner"
	"github.com/filecoin-project/go-state-types/crypto"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/types"
)

// ===========================================================================
// DoConsensusFault (Miner Slashing — Block Equivocation)
//
// Fabricates a consensus fault by creating two distinct blocks signed by the
// same miner at the same epoch (block equivocation). Submits the fault via
// ReportConsensusFault to slash the miner. Post-slash liveness and state
// consistency are verified by the existing consensus vectors
// (DoHeightProgression, DoTipsetConsensus, DoStateRootComparison, etc.)
//
// Target selection is configurable via STRESS_CONSENSUS_FAULT_TARGET:
//   "weakest"  — slash the miner with least raw power
//   "strongest" — slash the miner with most raw power (maximum disruption)
//   "random"   — pick randomly (default)
//
// Safety: fires at most once per miner, never slashes the last active miner.
// ===========================================================================

const (
	consensusFaultMinHeight   = 30
	consensusFaultMaxLookback = 5 // tipsets to search for a block by the target miner
)

var (
	slashedMiners   = make(map[address.Address]bool)
	slashedMinersMu sync.Mutex
)

func DoConsensusFault() {
	if len(nodeKeys) < 2 {
		return
	}

	// Guard: chain must be established
	head, err := nodes[nodeKeys[0]].ChainHead(ctx)
	if err != nil {
		return
	}
	if head.Height() < consensusFaultMinHeight {
		debugLog("[consensus-fault] chain height %d < %d, waiting", head.Height(), consensusFaultMinHeight)
		time.Sleep(2 * time.Second)
		return
	}

	// Pick a lotus node (forest can't WalletSign)
	lotusNode, lotusName := pickLotusNode()
	if lotusNode == nil {
		log.Printf("[consensus-fault] no lotus node available")
		return
	}

	// Discover active miners
	miners, err := lotusNode.StateListMiners(ctx, types.EmptyTSK)
	if err != nil {
		log.Printf("[consensus-fault] StateListMiners failed: %v", err)
		return
	}

	// Filter out already-slashed miners
	slashedMinersMu.Lock()
	var eligible []address.Address
	for _, m := range miners {
		if !slashedMiners[m] {
			eligible = append(eligible, m)
		}
	}
	// Never slash the last miner — need at least 2 eligible
	if len(eligible) < 2 {
		slashedMinersMu.Unlock()
		log.Printf("[consensus-fault] skipping: only %d eligible miners remain (slashed=%d)", len(eligible), len(slashedMiners))
		time.Sleep(5 * time.Second)
		return
	}
	slashedMinersMu.Unlock()

	// Select target based on configured strategy
	target := selectTargetMiner(lotusNode, eligible)
	if target == address.Undef {
		return
	}

	log.Printf("[consensus-fault] target miner: %s", target)

	// Get the worker address for signing
	minerInfo, err := lotusNode.StateMinerInfo(ctx, target, types.EmptyTSK)
	if err != nil {
		log.Printf("[consensus-fault] StateMinerInfo failed for %s: %v", target, err)
		return
	}
	workerAddr := minerInfo.Worker

	// Find a recent block produced by this miner
	originalBlock := findMinerBlock(lotusNode, target, consensusFaultMaxLookback)
	if originalBlock == nil {
		log.Printf("[consensus-fault] no recent block found for miner %s in last %d tipsets", target, consensusFaultMaxLookback)
		return
	}

	log.Printf("[consensus-fault] found block by %s at height %d, fabricating equivocation", target, originalBlock.Height)

	// Serialize the original block
	origBytes, err := originalBlock.Serialize()
	if err != nil {
		log.Printf("[consensus-fault] serialize original block failed: %v", err)
		return
	}

	// Create the forged block: copy and modify ForkSignaling
	forgedBlock := *originalBlock
	forgedBlock.ForkSignaling = originalBlock.ForkSignaling + 1
	forgedBlock.BlockSig = nil // clear sig before signing

	// Get signing bytes and re-sign with the miner's worker key
	forgedSigningBytes, err := forgedBlock.SigningBytes()
	if err != nil {
		log.Printf("[consensus-fault] SigningBytes failed: %v", err)
		return
	}

	sig, err := walletSignOnLotusNode(workerAddr, forgedSigningBytes)
	if err != nil {
		log.Printf("[consensus-fault] WalletSign failed: %v", err)
		return
	}
	forgedBlock.BlockSig = sig

	// Serialize the forged block
	forgedBytes, err := forgedBlock.Serialize()
	if err != nil {
		log.Printf("[consensus-fault] serialize forged block failed: %v", err)
		return
	}

	// Build ReportConsensusFault params
	params := miner15.ReportConsensusFaultParams{
		BlockHeader1: origBytes,
		BlockHeader2: forgedBytes,
	}
	serializedParams, err := actors.SerializeParams(&params)
	if err != nil {
		log.Printf("[consensus-fault] SerializeParams failed: %v", err)
		return
	}

	// Pick a reporter wallet (anyone can report a fault and collect reward)
	reporter, reporterKI := pickWallet()

	msg := &types.Message{
		From:   reporter,
		To:     target,
		Value:  abi.NewTokenAmount(0),
		Method: builtintypes.MethodsMiner.ReportConsensusFault,
		Params: serializedParams,
	}

	log.Printf("[consensus-fault] submitting ReportConsensusFault against %s from %s via %s", target, reporter, lotusName)

	msgCid, ok := pushContractMsg(lotusNode, msg, reporterKI, "consensus-fault")
	if !ok {
		return
	}

	// Wait for confirmation
	result := waitForMsg(lotusNode, msgCid, "consensus-fault")
	if result == nil {
		log.Printf("[consensus-fault] message not confirmed within timeout")
		return
	}

	slashSuccess := result.Receipt.ExitCode.IsSuccess()
	if !slashSuccess {
		log.Printf("[consensus-fault] ReportConsensusFault rejected: exit=%d", result.Receipt.ExitCode)
		return
	}

	// Mark miner as slashed
	slashedMinersMu.Lock()
	slashedMiners[target] = true
	slashedMinersMu.Unlock()

	log.Printf("[consensus-fault] miner %s successfully slashed!", target)
}

// selectTargetMiner picks a miner to slash based on STRESS_CONSENSUS_FAULT_TARGET.
//
//	"weakest"   — least raw power (preserves chain liveness)
//	"strongest"  — most raw power (maximum disruption)
//	"random"     — random selection (default)
func selectTargetMiner(node api.FullNode, eligible []address.Address) address.Address {
	strategy := envOrDefault("STRESS_CONSENSUS_FAULT_TARGET", "random")

	if strategy == "random" {
		target := rngChoice(eligible)
		log.Printf("[consensus-fault] target selection: random → %s", target)
		return target
	}

	type minerPower struct {
		addr  address.Address
		power abi.StoragePower
	}
	var miners []minerPower
	for _, m := range eligible {
		p, err := node.StateMinerPower(ctx, m, types.EmptyTSK)
		if err != nil {
			log.Printf("[consensus-fault] StateMinerPower failed for %s: %v", m, err)
			continue
		}
		miners = append(miners, minerPower{addr: m, power: p.MinerPower.RawBytePower})
	}
	if len(miners) == 0 {
		return address.Undef
	}

	selected := miners[0]
	for _, m := range miners[1:] {
		switch strategy {
		case "weakest":
			if m.power.LessThan(selected.power) {
				selected = m
			}
		case "strongest":
			if selected.power.LessThan(m.power) {
				selected = m
			}
		}
	}

	log.Printf("[consensus-fault] target selection: %s → %s (power=%s)", strategy, selected.addr, selected.power)
	return selected.addr
}

// findMinerBlock searches recent tipsets for a block produced by the given miner.
// Starts from the parent of ChainHead (not head itself) because
// ReportConsensusFault requires the fault epoch <= current execution epoch,
// and gas estimation runs at head.Height()-1.
func findMinerBlock(node api.FullNode, miner address.Address, maxDepth int) *types.BlockHeader {
	head, err := node.ChainHead(ctx)
	if err != nil {
		return nil
	}

	// Start from grandparent of head: gas estimation runs at head.Height(),
	// and the miner actor requires fault epoch < current epoch (strictly less).
	parentTs, err := node.ChainGetTipSet(ctx, head.Parents())
	if err != nil {
		return nil
	}
	ts, err := node.ChainGetTipSet(ctx, parentTs.Parents())
	if err != nil {
		return nil
	}

	for depth := 0; depth < maxDepth && ts != nil; depth++ {
		for _, blk := range ts.Blocks() {
			if blk.Miner == miner {
				return blk
			}
		}
		ts, err = node.ChainGetTipSet(ctx, ts.Parents())
		if err != nil {
			return nil
		}
	}
	return nil
}

// walletSignOnLotusNode tries WalletSign on each lotus node until one succeeds.
// Only the node that imported the miner's worker key will succeed.
func walletSignOnLotusNode(workerAddr address.Address, data []byte) (*crypto.Signature, error) {
	for _, name := range nodeKeys {
		if nodeType(name) != "lotus" {
			continue
		}
		sig, err := nodes[name].WalletSign(ctx, workerAddr, data)
		if err == nil {
			debugLog("[consensus-fault] signed with worker key on %s", name)
			return sig, nil
		}
		debugLog("[consensus-fault] WalletSign failed on %s: %v", name, err)
	}
	return nil, fmt.Errorf("no lotus node holds worker key for %s", workerAddr)
}

// pickLotusNode returns a lotus (non-forest) node for operations requiring WalletSign.
func pickLotusNode() (api.FullNode, string) {
	var lotusNodes []string
	for _, name := range nodeKeys {
		if nodeType(name) == "lotus" {
			lotusNodes = append(lotusNodes, name)
		}
	}
	if len(lotusNodes) == 0 {
		return nil, ""
	}
	name := rngChoice(lotusNodes)
	return nodes[name], name
}
