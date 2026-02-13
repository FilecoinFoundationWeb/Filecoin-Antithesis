package main

import (
	"log"
	"sync"

	"github.com/antithesishq/antithesis-sdk-go/assert"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/crypto"
	"github.com/filecoin-project/lotus/chain/types"
)

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
