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
// ReportConsensusFault to slash the miner. Fires once per run, random target.
//
// Post-slash liveness and state consistency are verified by the existing
// consensus vectors (DoHeightProgression, DoTipsetConsensus, etc.)
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

	// consensusFaultEnabled is decided once at startup by Antithesis RNG.
	// Some runs slash a miner, some don't — maximizes exploration.
	consensusFaultEnabled bool
)

// initConsensusFault decides whether consensus fault is active for this run.
// Called once from main() before buildDeck().
func initConsensusFault() {
	consensusFaultEnabled = rngIntn(2) == 0 // 50/50 chance
	log.Printf("[consensus-fault] enabled for this run: %v", consensusFaultEnabled)
}

func DoConsensusFault() {
	if !consensusFaultEnabled {
		return
	}
	if len(nodeKeys) < 2 {
		return
	}

	head, err := nodes[nodeKeys[0]].ChainHead(ctx)
	if err != nil {
		return
	}
	if head.Height() < consensusFaultMinHeight {
		debugLog("[consensus-fault] chain height %d < %d, waiting", head.Height(), consensusFaultMinHeight)
		time.Sleep(2 * time.Second)
		return
	}

	lotusNode, lotusName := pickLotusNode()
	if lotusNode == nil {
		log.Printf("[consensus-fault] no lotus node available")
		return
	}

	eligible := getEligibleMiners(lotusNode)
	if eligible == nil {
		time.Sleep(5 * time.Second)
		return
	}

	target := rngChoice(eligible)
	log.Printf("[consensus-fault] target miner: %s", target)

	minerInfo, err := lotusNode.StateMinerInfo(ctx, target, types.EmptyTSK)
	if err != nil {
		log.Printf("[consensus-fault] StateMinerInfo failed for %s: %v", target, err)
		return
	}

	// Find a recent block by this miner (grandparent of head to avoid epoch-ahead error)
	originalBlock := findMinerBlock(lotusNode, target, consensusFaultMaxLookback)
	if originalBlock == nil {
		log.Printf("[consensus-fault] no recent block found for miner %s in last %d tipsets", target, consensusFaultMaxLookback)
		return
	}

	log.Printf("[consensus-fault] found block by %s at height %d, fabricating equivocation", target, originalBlock.Height)

	origBytes, err := originalBlock.Serialize()
	if err != nil {
		log.Printf("[consensus-fault] serialize original block failed: %v", err)
		return
	}

	// Forge block: copy, bump ForkSignaling, re-sign
	forgedBlock := *originalBlock
	forgedBlock.ForkSignaling = originalBlock.ForkSignaling + 1
	forgedBlock.BlockSig = nil

	forgedSigningBytes, err := forgedBlock.SigningBytes()
	if err != nil {
		log.Printf("[consensus-fault] SigningBytes failed: %v", err)
		return
	}

	sig, err := walletSignOnLotusNode(minerInfo.Worker, forgedSigningBytes)
	if err != nil {
		log.Printf("[consensus-fault] WalletSign failed: %v", err)
		return
	}
	forgedBlock.BlockSig = sig

	forgedBytes, err := forgedBlock.Serialize()
	if err != nil {
		log.Printf("[consensus-fault] serialize forged block failed: %v", err)
		return
	}

	params := miner15.ReportConsensusFaultParams{
		BlockHeader1: origBytes,
		BlockHeader2: forgedBytes,
	}
	serializedParams, err := actors.SerializeParams(&params)
	if err != nil {
		log.Printf("[consensus-fault] SerializeParams failed: %v", err)
		return
	}

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

	result := waitForMsg(lotusNode, msgCid, "consensus-fault")
	if result == nil {
		log.Printf("[consensus-fault] message not confirmed within timeout")
		return
	}

	if !result.Receipt.ExitCode.IsSuccess() {
		log.Printf("[consensus-fault] ReportConsensusFault rejected: exit=%d", result.Receipt.ExitCode)
		return
	}

	slashedMinersMu.Lock()
	slashedMiners[target] = true
	slashedMinersMu.Unlock()

	log.Printf("[consensus-fault] miner %s successfully slashed!", target)
}

// ===========================================================================
// Shared helpers
// ===========================================================================

// getEligibleMiners returns miners not yet slashed by consensus fault.
// Returns nil if fewer than 2 eligible (must preserve at least one active miner).
func getEligibleMiners(node api.FullNode) []address.Address {
	miners, err := node.StateListMiners(ctx, types.EmptyTSK)
	if err != nil {
		log.Printf("[consensus-fault] StateListMiners failed: %v", err)
		return nil
	}

	slashedMinersMu.Lock()
	defer slashedMinersMu.Unlock()

	var eligible []address.Address
	for _, m := range miners {
		if !slashedMiners[m] {
			eligible = append(eligible, m)
		}
	}
	if len(eligible) < 2 {
		debugLog("[consensus-fault] only %d eligible miners, need >= 2", len(eligible))
		return nil
	}
	return eligible
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

// walletSignOnLotusNode tries WalletSign on each lotus node until one succeeds.
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

// findMinerBlock searches recent tipsets for a block produced by the given miner.
// Starts from grandparent of head (fault epoch must be < execution epoch).
func findMinerBlock(node api.FullNode, miner address.Address, maxDepth int) *types.BlockHeader {
	head, err := node.ChainHead(ctx)
	if err != nil {
		return nil
	}

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
