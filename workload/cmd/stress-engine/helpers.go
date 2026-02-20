package main

import (
	"log"
	"os"

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
