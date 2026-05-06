package main

import (
	"bytes"
	"context"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/antithesishq/antithesis-sdk-go/assert"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	initact "github.com/filecoin-project/go-state-types/builtin/v15/init"
	multisigact "github.com/filecoin-project/go-state-types/builtin/v15/multisig"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
)

// =============================================================================
// DoMsigF410ApproveAudit — Multisig approval from a delegated (f410) signer
//
// SCOPE: sandboxed local devnet test only.
//
// This vector is a port of the parallel_driver_multisig scenario from the
// upstream Lotus antithesis-test workload, adapted to this harness's
// stress-engine vector style. Its sole purpose is to detect regressions of a
// known SUT bug in Lotus's local-keystore signing path.
//
// Bug under test: `MsigApprove` from an f410 (Ethereum-derived,
// `KTDelegated`) Filecoin address fails because
// `chain/messagesigner/messagesigner.go:SigningBytes` unconditionally tries to
// reconstruct the message as an EIP-1559 Ethereum transaction whenever the
// sender is f410. When the message targets a Filecoin-native built-in actor
// (the multisig actor f05), the params are CBOR-encoded native arguments, not
// RLP-encoded Eth tx fields, so reconstruction fails with:
//
//   failed to reconstruct eth transaction: ...
//   failed to read params byte array: expected cbor type 'byte string' in input
//
// The assertion fires exclusively on that error signature. Any other error
// (RPC unavailable, gas estimation failure, partition-induced timeout) is
// logged as a diagnostic and does not fail.
//
// Test-isolation properties (deliberately narrow):
//   - All keys used by this vector are generated in-process via WalletNew on
//     a single lotus node. No external/user keys are imported.
//   - Funding is pulled from the harness's own pre-funded devnet deck wallets.
//   - The multisig is freshly created inside the devnet; this vector never
//     touches mainnet, real funds, or any external system.
//   - The proposed transaction is a 0-FIL transfer to the multisig itself
//     (`To = msigAddr, Value = 0, Method = 0`) so successful execution is a
//     no-op on chain.
//   - Vector is gated behind STRESS_WEIGHT_MSIG_F410_APPROVE (default 0) so
//     it only runs when explicitly enabled in a profile's .env.
//
// =============================================================================

const (
	msigSignerFundFIL    = 5  // FIL handed to each generated signer for gas
	msigInitMaxAttempts  = 3  // retries for initial wallet generation / fund / create
	msigProposeWaitConfs = 1  // confirmations for StateWaitMsg on propose
	msigApproveWaitConfs = 1
)

var (
	msigSetupOnce  sync.Once
	msigReady      atomic.Bool
	msigDisabled   atomic.Bool

	msigNode       api.FullNode
	msigNodeName   string
	msigT1Signer   address.Address // node-local secp256k1 signer (proposer)
	msigF410Signer address.Address // node-local delegated signer (the SUT-bug target)
	msigAddr       address.Address // robust address of the freshly created multisig

	msigBugSeen      atomic.Bool   // sticky: set on first observed bug signature
)

// DoMsigF410ApproveAudit is the deck entry point for this vector.
func DoMsigF410ApproveAudit() {
	if msigDisabled.Load() {
		return
	}
	// One-shot lazy init. If init fails, msigDisabled is set and we never
	// retry — better to skip than to spam the log with the same setup error.
	msigSetupOnce.Do(initMsigF410)
	if !msigReady.Load() {
		return
	}

	// Skip during active partitions. The bug we want to catch is a
	// deterministic SUT signing-path failure, not a transient network
	// issue, so don't risk false-positive matches against partition
	// timeouts.
	if partitionActive.Load() {
		return
	}

	runMsigF410ApproveCycle()
}

// -----------------------------------------------------------------------------
// One-time setup: generate two signers locally on a lotus node, fund them,
// and create a 2-of-2 multisig. Everything here uses the harness's existing
// pre-funded deck wallets as the source of FIL.
// -----------------------------------------------------------------------------

func initMsigF410() {
	node, name := pickLotusNode()
	if node == nil {
		log.Println("[msig-f410] no lotus node available — vector disabled")
		msigDisabled.Store(true)
		return
	}

	// Generate signer keys directly on the chosen node. This is essential:
	// the bug is in the node-side signing path, so the keys MUST live in
	// the node's local keystore for the failure to be reachable.
	t1, err := node.WalletNew(ctx, types.KTSecp256k1)
	if err != nil {
		log.Printf("[msig-f410] WalletNew(secp256k1) failed on %s: %v", name, err)
		msigDisabled.Store(true)
		return
	}
	f410, err := node.WalletNew(ctx, types.KTDelegated)
	if err != nil {
		log.Printf("[msig-f410] WalletNew(delegated) failed on %s: %v", name, err)
		msigDisabled.Store(true)
		return
	}
	log.Printf("[msig-f410] generated signers on %s: t1=%s f410=%s", name, t1, f410)

	// Fund both signers from a deck wallet so they can pay gas.
	// The f410 signer must also receive at least one inbound transfer
	// before the chain has registered its account actor; this happens
	// implicitly here.
	fundAmt := types.FromFil(uint64(msigSignerFundFIL))
	if !fundFromDeck(node, t1, fundAmt, "msig-f410/fund-t1") {
		msigDisabled.Store(true)
		return
	}
	if !fundFromDeck(node, f410, fundAmt, "msig-f410/fund-f410") {
		msigDisabled.Store(true)
		return
	}

	// Create the 2-of-2 multisig. Sender is t1 (secp256k1) — sending the
	// create from the f410 sender would itself trip the same bug and we'd
	// never finish setup.
	proto, err := node.MsigCreate(ctx,
		2, // requiredApprovals
		[]address.Address{t1, f410},
		abi.ChainEpoch(0), // unlock duration
		big.Zero(),        // initial balance
		t1,                // create sender
		big.Zero(),        // gas price (auto)
	)
	if err != nil {
		log.Printf("[msig-f410] MsigCreate prototype failed: %v", err)
		msigDisabled.Store(true)
		return
	}

	smsg, err := node.MpoolPushMessage(ctx, &proto.Message, nil)
	if err != nil {
		log.Printf("[msig-f410] MsigCreate push failed: %v", err)
		msigDisabled.Store(true)
		return
	}

	tctx, tcancel := context.WithTimeout(ctx, 3*time.Minute)
	defer tcancel()
	look, err := node.StateWaitMsg(tctx, smsg.Cid(), 1, 200, true)
	if err != nil || look == nil {
		log.Printf("[msig-f410] MsigCreate wait failed: %v", err)
		msigDisabled.Store(true)
		return
	}
	if look.Receipt.ExitCode != 0 {
		log.Printf("[msig-f410] MsigCreate non-zero exit: %d", look.Receipt.ExitCode)
		msigDisabled.Store(true)
		return
	}

	var execRet initact.ExecReturn
	if err := execRet.UnmarshalCBOR(bytes.NewReader(look.Receipt.Return)); err != nil {
		log.Printf("[msig-f410] MsigCreate return decode failed: %v", err)
		msigDisabled.Store(true)
		return
	}
	log.Printf("[msig-f410] multisig created: id=%s robust=%s",
		execRet.IDAddress, execRet.RobustAddress)

	msigNode = node
	msigNodeName = name
	msigT1Signer = t1
	msigF410Signer = f410
	msigAddr = execRet.RobustAddress
	msigReady.Store(true)
}

// fundFromDeck sends `amt` from a randomly chosen deck wallet to `to` and
// waits for inclusion. Returns true on success.
func fundFromDeck(node api.FullNode, to address.Address, amt abi.TokenAmount, tag string) bool {
	for attempt := 0; attempt < msigInitMaxAttempts; attempt++ {
		from, ki := pickWallet()
		msg := baseMsg(from, to, amt)
		estimateGas(node, msg, tag)

		msgCid, ok := pushMsgWithCid(node, msg, ki, tag)
		if !ok {
			continue
		}
		if look := waitForMsg(node, msgCid, tag); look != nil && look.Receipt.ExitCode == 0 {
			return true
		}
	}
	log.Printf("[%s] failed to fund %s after %d attempts", tag, to, msigInitMaxAttempts)
	return false
}

// -----------------------------------------------------------------------------
// Per-iteration: propose a 0-FIL transfer from t1, then approve from f410.
// The approve is the SUT-bug target.
// -----------------------------------------------------------------------------

func runMsigF410ApproveCycle() {
	// Step 1 — propose from the t1 signer. method=0 + value=0 means the
	// approved-and-executed transaction is a no-op on chain.
	proposeProto, err := msigNode.MsigPropose(ctx,
		msigAddr,
		msigAddr,    // recipient: send to msig itself (no-op transfer)
		big.Zero(),  // value
		msigT1Signer,
		0,    // method
		nil,  // params
	)
	if err != nil {
		debugLog("[msig-f410] MsigPropose prototype failed: %v", err)
		return
	}
	proposeSigned, err := msigNode.MpoolPushMessage(ctx, &proposeProto.Message, nil)
	if err != nil {
		debugLog("[msig-f410] MsigPropose push failed: %v", err)
		return
	}

	tctx, tcancel := context.WithTimeout(ctx, 2*time.Minute)
	defer tcancel()
	proposeLook, err := msigNode.StateWaitMsg(tctx, proposeSigned.Cid(), msigProposeWaitConfs, 200, true)
	if err != nil || proposeLook == nil {
		debugLog("[msig-f410] MsigPropose wait failed: %v", err)
		return
	}
	if proposeLook.Receipt.ExitCode != 0 {
		debugLog("[msig-f410] MsigPropose non-zero exit: %d", proposeLook.Receipt.ExitCode)
		return
	}

	var proposeRet multisigact.ProposeReturn
	if err := proposeRet.UnmarshalCBOR(bytes.NewReader(proposeLook.Receipt.Return)); err != nil {
		debugLog("[msig-f410] MsigPropose return decode failed: %v", err)
		return
	}
	txnID := uint64(proposeRet.TxnID)

	// Step 2 — approve from the f410 signer. THIS is the call exercising
	// the SUT bug. The v1 API just returns a prototype; the failure (if
	// the bug is present) surfaces inside MpoolPushMessage when the
	// node's MessageSigner.SignMessage → SigningBytes path runs.
	approveProto, approveProtoErr := msigNode.MsigApprove(ctx, msigAddr, txnID, msigF410Signer)
	if approveProtoErr != nil {
		// Some lotus versions surface the same SigningBytes path through
		// the prototype RPC itself (depending on wrapper layering). Match
		// the bug signature here too.
		handleApproveError(approveProtoErr, txnID, "MsigApprove (proto)")
		return
	}

	_, approvePushErr := msigNode.MpoolPushMessage(ctx, &approveProto.Message, nil)
	if approvePushErr != nil {
		handleApproveError(approvePushErr, txnID, "MpoolPushMessage (approve)")
		return
	}

	// Approve push accepted by the mempool — the bug-triggering signing
	// path was traversed without failure. Record happy-path coverage.
	assert.Sometimes(true,
		"MsigApprove from f410 signer accepted by message-signer path",
		map[string]any{
			"node":   msigNodeName,
			"msig":   msigAddr.String(),
			"txn_id": txnID,
		})
}

// handleApproveError inspects an MsigApprove-related error and fires the
// SUT-bug assertion if the error matches the known signature. All other
// errors are diagnostic-only.
func handleApproveError(err error, txnID uint64, where string) {
	bug := isF410ReconstructionError(err)
	details := map[string]any{
		"node":      msigNodeName,
		"msig":      msigAddr.String(),
		"txn_id":    txnID,
		"signer":    msigF410Signer.String(),
		"where":     where,
		"err":       err.Error(),
		"bug_match": bug,
	}

	if bug {
		msigBugSeen.Store(true)
		log.Printf("[msig-f410] BUG SIGNATURE: %s: %v", where, err)
	} else {
		debugLog("[msig-f410] approve error (non-bug): %s: %v", where, err)
	}

	// Always: a multisig approve from an f410 signer must not fail with
	// the eth-tx reconstruction signature. Any match => the SUT bug
	// has regressed.
	assert.Always(!bug,
		"MsigApprove from f410 signer not rejected by eth-tx reconstruction",
		details)
}

// isF410ReconstructionError matches the error string emitted by the buggy
// path in messagesigner.SigningBytes. The wording is preserved verbatim from
// upstream Lotus, but we accept any of the substrings that have been seen
// across affected versions to stay tolerant to minor wrapping changes.
func isF410ReconstructionError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	signatures := []string{
		"failed to reconstruct eth transaction",
		"failed to get eth params and recipient",
		"failed to read params byte array",
		"expected cbor type 'byte string' in input",
	}
	for _, sig := range signatures {
		if strings.Contains(s, sig) {
			return true
		}
	}
	return false
}
