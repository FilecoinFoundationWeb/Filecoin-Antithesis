package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/antithesishq/antithesis-sdk-go/assert"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/crypto"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/lib/sigs"
	"github.com/ipfs/go-cid"
)

// debugLogging gates verbose per-action logs. Set STRESS_DEBUG=1 to enable.
var debugLogging = os.Getenv("STRESS_DEBUG") == "1"

// debugLog prints only when STRESS_DEBUG=1 is set.
func debugLog(format string, args ...any) {
	if debugLogging {
		log.Printf(format, args...)
	}
}

// ===========================================================================
// Shared message helpers
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

// errStr safely converts an error to string for assertion details.
func errStr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// cidStr returns a short string representation of a CID.
func cidStr(c cid.Cid) string {
	s := c.String()
	if len(s) > 16 {
		return s[:16]
	}
	return s
}

// getContractsByType returns all deployed contracts of a given type.
func getContractsByType(ctype string) []deployedContract {
	contractsMu.Lock()
	defer contractsMu.Unlock()
	var result []deployedContract
	for _, c := range deployedContracts {
		if c.ctype == ctype {
			result = append(result, c)
		}
	}
	return result
}

// ===========================================================================
// Cross-node & lifecycle helpers
// ===========================================================================

const defaultWaitTimeout = 2 * time.Minute

// waitForMsg wraps StateWaitMsg with a timeout. Returns nil on failure.
func waitForMsg(node api.FullNode, msgCid cid.Cid, tag string) *api.MsgLookup {
	tctx, tcancel := context.WithTimeout(ctx, defaultWaitTimeout)
	defer tcancel()

	result, err := node.StateWaitMsg(tctx, msgCid, 1, 200, true)
	if err != nil {
		log.Printf("[%s] StateWaitMsg failed for %s: %v", tag, cidStr(msgCid), err)
		return nil
	}
	return result
}

// pushMsgWithCid signs and pushes a message, returning its CID.
// Manages nonces: increments only on success.
func pushMsgWithCid(node api.FullNode, msg *types.Message, ki *types.KeyInfo, tag string) (cid.Cid, bool) {
	msg.Nonce = nonces[msg.From]

	smsg := signMsg(msg, ki)
	if smsg == nil {
		return cid.Undef, false
	}

	msgCid, err := node.MpoolPush(ctx, smsg)
	if err != nil {
		log.Printf("[%s] MpoolPush failed: %v", tag, err)
		return cid.Undef, false
	}

	nonces[msg.From]++
	return msgCid, true
}

// pushMsgManualNonce signs and pushes with an explicit nonce.
// Does NOT touch the global nonces map — caller manages nonces.
func pushMsgManualNonce(node api.FullNode, msg *types.Message, ki *types.KeyInfo, nonce uint64, tag string) (cid.Cid, bool) {
	msg.Nonce = nonce

	smsg := signMsg(msg, ki)
	if smsg == nil {
		return cid.Undef, false
	}

	msgCid, err := node.MpoolPush(ctx, smsg)
	if err != nil {
		debugLog("[%s] MpoolPush (nonce=%d) failed: %v", tag, nonce, err)
		return cid.Undef, false
	}
	return msgCid, true
}

// pickTwoDistinctNodes returns two different nodes. Returns empty strings if <2 nodes.
func pickTwoDistinctNodes() (string, string, api.FullNode, api.FullNode) {
	if len(nodeKeys) < 2 {
		return "", "", nil, nil
	}
	idxA := rngIntn(len(nodeKeys))
	idxB := (idxA + 1 + rngIntn(len(nodeKeys)-1)) % len(nodeKeys)
	nameA, nameB := nodeKeys[idxA], nodeKeys[idxB]
	return nameA, nameB, nodes[nameA], nodes[nameB]
}

// verifyActorConsistency checks StateGetActor at the minimum finalized tipset
// across all nodes. Skips nodes that error (may be lagging/disconnected).
// Asserts only when 2+ nodes respond successfully.
func verifyActorConsistency(addr address.Address, phase string) {
	finHeight, finTsk := getFinalizedHeight()
	if finHeight < finalizedMinHeight {
		return
	}

	type result struct {
		node  string
		state string
	}
	var results []result

	for _, name := range nodeKeys {
		actor, err := nodes[name].StateGetActor(ctx, addr, finTsk)
		if err != nil {
			debugLog("[actor-verify] %s: StateGetActor failed on %s: %v", phase, name, err)
			continue // skip lagging/disconnected nodes
		}
		if actor == nil {
			results = append(results, result{node: name, state: "nil"})
		} else {
			results = append(results, result{node: name, state: fmt.Sprintf("code=%s,nonce=%d,balance=%s", actor.Code, actor.Nonce, actor.Balance)})
		}
	}

	if len(results) < 2 {
		return // not enough responsive nodes to compare
	}

	allMatch := true
	for i := 1; i < len(results); i++ {
		if results[i].state != results[0].state {
			allMatch = false
			break
		}
	}

	details := make(map[string]any)
	details["actor"] = addr.String()
	details["phase"] = phase
	details["finalized_height"] = finHeight
	details["respondents"] = len(results)
	for _, r := range results {
		details["state_"+r.node] = r.state
	}

	assert.Sometimes(allMatch, "Actor state consistent across nodes", details)

	if !allMatch {
		log.Printf("[actor-verify] DIVERGENCE %s actor=%s: %v", phase, addr, details)
	}
}
