package main

import (
	"log"
	"sync"
	"time"

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
		debugLog("  [transfer] OK: %s -> %s via %s (amount=%s)",
			fromAddr.String()[:12], toAddr.String()[:12], nodeName, amount.String())
	}
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

	_, node := pickNode()
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

	debugLog("  [gas-war] nonce=%d: Tx_A(low)=%v, Tx_B(high)=%v",
		currentNonce, errA == nil, errB == nil)
}

// doDoubleSpend sends conflicting transactions (same nonce, different recipients)
// to two different nodes. Asserts at most one should be included on-chain.
func doDoubleSpend() {
	if len(nodeKeys) < 2 {
		return
	}
	// Skip during intentional partitions — both txs could land on different
	// forks, creating a false positive after heal.
	if partitionActive.Load() {
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

	if errA != nil && errB != nil {
		debugLog("[adversarial] double-spend: both pushes failed (nodeA=%v, nodeB=%v)", errA, errB)
		return
	}

	// On-chain verification: at most one same-nonce tx should land
	cidA := smsgA.Cid()
	cidB := smsgB.Cid()
	refNode := nodes[nodeKeys[0]]

	// Wait for chain to advance 5 epochs rather than wall-clock time.
	// Antithesis runs on a single core with time dilation, so wall-clock
	// sleeps don't reliably correspond to block production.
	startHead, headErr := refNode.ChainHead(ctx)
	chainAdvanced := false
	if headErr == nil {
		targetHeight := startHead.Height() + 5
		deadline := time.After(120 * time.Second)
	waitLoop:
		for {
			select {
			case <-deadline:
				break waitLoop
			case <-time.After(2 * time.Second):
				head, err := refNode.ChainHead(ctx)
				if err == nil && head.Height() >= targetHeight {
					chainAdvanced = true
					break waitLoop
				}
			}
		}
	} else {
		// Fallback: if ChainHead fails, sleep and hope
		time.Sleep(30 * time.Second)
	}

	// allowReplaced=true: the nonce may have been consumed by a replacement msg
	resultA, _ := refNode.StateSearchMsg(ctx, types.EmptyTSK, cidA, 200, true)
	resultB, _ := refNode.StateSearchMsg(ctx, types.EmptyTSK, cidB, 200, true)

	aLanded := resultA != nil && resultA.Receipt.ExitCode.IsSuccess()
	bLanded := resultB != nil && resultB.Receipt.ExitCode.IsSuccess()

	landed := 0
	if aLanded {
		landed++
	}
	if bLanded {
		landed++
	}

	safe := landed <= 1
	assert.Always(safe, "At most one double-spend tx lands on chain", map[string]any{
		"from":     fromAddr.String(),
		"nonce":    currentNonce,
		"node_a":   nodeA,
		"node_b":   nodeB,
		"a_landed": aLanded,
		"b_landed": bLanded,
		"total":    landed,
	})

	if landed > 1 {
		log.Printf("[adversarial] DOUBLE-SPEND VIOLATION: both txs landed for nonce %d", currentNonce)
	}

	// Only assert liveness if the chain actually advanced — if no blocks were
	// produced during our wait (e.g., drand killed, partition), the assertion
	// is meaningless noise.
	if chainAdvanced {
		assert.Sometimes(landed >= 1, "At least one double-spend tx lands", map[string]any{
			"from":  fromAddr.String(),
			"nonce": currentNonce,
			"total": landed,
		})
	}

	debugLog("[adversarial] double-spend: nodeA=%s nodeB=%s landed=%d/2 chainAdvanced=%v", nodeA, nodeB, landed, chainAdvanced)
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

	assert.Always(rejected, "Message with invalid signature was rejected", map[string]any{
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

	wg.Add(2)
	go func() {
		defer wg.Done()
		nodes[nodeA].MpoolPush(ctx, smsgLow)
	}()
	go func() {
		defer wg.Done()
		nodes[nodeB].MpoolPush(ctx, smsgHigh)
	}()
	wg.Wait()

	nonces[fromAddr]++
}
