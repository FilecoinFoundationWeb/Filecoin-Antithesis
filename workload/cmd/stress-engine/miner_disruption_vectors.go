package main

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-bitfield"
	"github.com/filecoin-project/go-state-types/abi"
	builtintypes "github.com/filecoin-project/go-state-types/builtin"
	miner15 "github.com/filecoin-project/go-state-types/builtin/v15/miner"
	"github.com/filecoin-project/go-state-types/crypto"
	prooftypes "github.com/filecoin-project/go-state-types/proof"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/ipfs/go-cid"
)

// ===========================================================================
// Miner Disruption Vectors
//
// Two mutually exclusive strategies, one chosen randomly per simulation run:
//
//   consensus_fault — permanent slash via fabricated block equivocation.
//                     Fires once per run, one miner.
//
//   invalid_post   — temporary fault via empty PoSt proof submission.
//                     Fires periodically (every invalidPostCooldownEpochs),
//                     random miner each time. Miner auto-recovers.
//
// The mode is selected at startup by initMinerDisruption() using Antithesis
// RNG. The unchosen vector never fires.
// ===========================================================================

const (
	minerDisruptionMinHeight     = 30
	consensusFaultMaxLookback    = 5   // tipsets to search for a block by the target miner
	invalidPostCooldownEpochs    = 100 // ~7 min at 4s block time
)

var (
	// Mode for this run: "consensus_fault" or "invalid_post"
	minerDisruptionMode string

	// Consensus fault state: tracks permanently slashed miners
	slashedMiners   = make(map[address.Address]bool)
	slashedMinersMu sync.Mutex

	// Invalid PoSt state: cooldown tracking
	lastInvalidPostEpoch abi.ChainEpoch
)

// initMinerDisruption picks the disruption mode for this run.
// Called once from main() before buildDeck().
func initMinerDisruption() {
	minerDisruptionMode = rngChoice([]string{"consensus_fault", "invalid_post"})
	log.Printf("[miner-disruption] mode chosen for this run: %s", minerDisruptionMode)
}

// DoMinerDisruption dispatches to the chosen strategy.
func DoMinerDisruption() {
	switch minerDisruptionMode {
	case "consensus_fault":
		doConsensusFault()
	case "invalid_post":
		doInvalidWindowPoSt()
	}
}

// ===========================================================================
// Shared helpers
// ===========================================================================

// getEligibleMiners returns miners not yet slashed by consensus fault.
// Returns nil if fewer than 2 eligible (must preserve at least one active miner).
func getEligibleMiners(node api.FullNode) []address.Address {
	miners, err := node.StateListMiners(ctx, types.EmptyTSK)
	if err != nil {
		log.Printf("[miner-disruption] StateListMiners failed: %v", err)
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
		debugLog("[miner-disruption] only %d eligible miners, need >= 2", len(eligible))
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
			debugLog("[miner-disruption] signed with worker key on %s", name)
			return sig, nil
		}
		debugLog("[miner-disruption] WalletSign failed on %s: %v", name, err)
	}
	return nil, fmt.Errorf("no lotus node holds worker key for %s", workerAddr)
}

// pushMsgFromWorker submits a message FROM a miner's worker address via
// MpoolPushMessage. Tries each lotus node — only the one holding the worker
// key will succeed (auto-signs internally).
func pushMsgFromWorker(workerAddr, toAddr address.Address, method abi.MethodNum, params []byte, tag string) (cid.Cid, bool) {
	msg := &types.Message{
		From:   workerAddr,
		To:     toAddr,
		Method: method,
		Value:  abi.NewTokenAmount(0),
		Params: params,
	}
	for _, name := range nodeKeys {
		if nodeType(name) != "lotus" {
			continue
		}
		smsg, err := nodes[name].MpoolPushMessage(ctx, msg, nil)
		if err == nil {
			log.Printf("[%s] submitted via %s, cid=%s", tag, name, smsg.Cid())
			return smsg.Cid(), true
		}
		debugLog("[%s] MpoolPushMessage failed on %s: %v", tag, name, err)
	}
	log.Printf("[%s] MpoolPushMessage failed on all lotus nodes", tag)
	return cid.Undef, false
}

// ===========================================================================
// DoConsensusFault (permanent slash, fires once)
// ===========================================================================

func doConsensusFault() {
	if len(nodeKeys) < 2 {
		return
	}

	head, err := nodes[nodeKeys[0]].ChainHead(ctx)
	if err != nil {
		return
	}
	if head.Height() < minerDisruptionMinHeight {
		debugLog("[consensus-fault] chain height %d < %d, waiting", head.Height(), minerDisruptionMinHeight)
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

// ===========================================================================
// DoInvalidWindowPoSt (temporary fault, fires periodically)
// ===========================================================================

func doInvalidWindowPoSt() {
	if len(nodeKeys) < 2 {
		return
	}

	head, err := nodes[nodeKeys[0]].ChainHead(ctx)
	if err != nil {
		return
	}
	if head.Height() < minerDisruptionMinHeight {
		debugLog("[invalid-post] chain height %d < %d, waiting", head.Height(), minerDisruptionMinHeight)
		time.Sleep(2 * time.Second)
		return
	}

	// Cooldown check
	if head.Height()-lastInvalidPostEpoch < invalidPostCooldownEpochs {
		debugLog("[invalid-post] cooldown: %d epochs remaining", invalidPostCooldownEpochs-(head.Height()-lastInvalidPostEpoch))
		time.Sleep(5 * time.Second)
		return
	}

	lotusNode, lotusName := pickLotusNode()
	if lotusNode == nil {
		log.Printf("[invalid-post] no lotus node available")
		return
	}

	eligible := getEligibleMiners(lotusNode)
	if eligible == nil {
		time.Sleep(5 * time.Second)
		return
	}

	target := rngChoice(eligible)
	log.Printf("[invalid-post] target miner: %s", target)

	// Get miner info for worker address and proof type
	minfo, err := lotusNode.StateMinerInfo(ctx, target, types.EmptyTSK)
	if err != nil {
		log.Printf("[invalid-post] StateMinerInfo failed for %s: %v", target, err)
		return
	}

	// Get current proving deadline
	deadline, err := lotusNode.StateMinerProvingDeadline(ctx, target, types.EmptyTSK)
	if err != nil {
		log.Printf("[invalid-post] StateMinerProvingDeadline failed for %s: %v", target, err)
		return
	}

	// Get partitions for this deadline
	partitions, err := lotusNode.StateMinerPartitions(ctx, target, deadline.Index, types.EmptyTSK)
	if err != nil {
		log.Printf("[invalid-post] StateMinerPartitions failed for %s deadline %d: %v", target, deadline.Index, err)
		return
	}
	if len(partitions) == 0 {
		debugLog("[invalid-post] no partitions for %s at deadline %d", target, deadline.Index)
		return
	}

	// Get chain commit randomness
	checkRand, err := lotusNode.StateGetRandomnessFromTickets(ctx,
		crypto.DomainSeparationTag_PoStChainCommit,
		deadline.Challenge, nil, head.Key())
	if err != nil {
		log.Printf("[invalid-post] StateGetRandomnessFromTickets failed: %v", err)
		return
	}

	// Build empty proof (zero bytes = invalid)
	proofSize, err := minfo.WindowPoStProofType.ProofSize()
	if err != nil {
		log.Printf("[invalid-post] ProofSize failed: %v", err)
		return
	}

	emptyProof := []prooftypes.PoStProof{{
		PoStProof:  minfo.WindowPoStProofType,
		ProofBytes: make([]byte, proofSize),
	}}

	// Build partition list
	var postPartitions []miner15.PoStPartition
	for i := range partitions {
		postPartitions = append(postPartitions, miner15.PoStPartition{
			Index:   uint64(i),
			Skipped: bitfield.New(),
		})
	}

	params := miner15.SubmitWindowedPoStParams{
		Deadline:         deadline.Index,
		Partitions:       postPartitions,
		Proofs:           emptyProof,
		ChainCommitEpoch: deadline.Challenge,
		ChainCommitRand:  checkRand,
	}

	serializedParams, err := actors.SerializeParams(&params)
	if err != nil {
		log.Printf("[invalid-post] SerializeParams failed: %v", err)
		return
	}

	log.Printf("[invalid-post] submitting invalid PoSt for %s (%d partitions, deadline %d) via %s",
		target, len(postPartitions), deadline.Index, lotusName)

	msgCid, ok := pushMsgFromWorker(minfo.Worker, target,
		builtintypes.MethodsMiner.SubmitWindowedPoSt, serializedParams, "invalid-post")
	if !ok {
		return
	}

	result := waitForMsg(lotusNode, msgCid, "invalid-post")
	if result == nil {
		log.Printf("[invalid-post] message not confirmed within timeout")
		return
	}

	if result.Receipt.ExitCode.IsSuccess() {
		log.Printf("[invalid-post] invalid PoSt accepted for %s — miner faulted!", target)
	} else {
		log.Printf("[invalid-post] invalid PoSt rejected for %s: exit=%d", target, result.Receipt.ExitCode)
	}

	// Update cooldown regardless of success (avoid hammering)
	lastInvalidPostEpoch = head.Height()
}
