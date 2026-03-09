package foc

import (
	"context"
	"log"
	"math/big"
	"sync"
	"time"

	"github.com/filecoin-project/go-address"
	filbig "github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/crypto"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/types/ethtypes"
	"github.com/filecoin-project/lotus/lib/sigs"
	_ "github.com/filecoin-project/lotus/lib/sigs/delegated" // register SigTypeDelegated signer
)

const (
	receiptPollInterval = 4 * time.Second
	receiptPollTimeout  = 2 * time.Minute
)

// EthNonces is a local nonce cache for EVM transactions to avoid concurrent
// goroutines fetching the same nonce from the node and colliding in the mpool.
var (
	EthNonces   = map[address.Address]uint64{}
	EthNoncesMu sync.Mutex
)

// BuildCalldata constructs ABI-encoded calldata from a 4-byte selector and pre-encoded args.
func BuildCalldata(selector []byte, args ...[]byte) []byte {
	buf := make([]byte, 0, 4+32*len(args))
	buf = append(buf, selector...)
	for _, arg := range args {
		buf = append(buf, arg...)
	}
	return buf
}

// sendEthTxCore signs and submits an EIP-1559 EVM transaction.
// Returns the tx hash and true if accepted by the mempool, or zero hash and false on failure.
func sendEthTxCore(ctx context.Context, node api.FullNode, privKey []byte, toAddr []byte, calldata []byte, tag string) (ethtypes.EthHash, bool) {
	var zero ethtypes.EthHash

	if len(privKey) != 32 {
		log.Printf("[%s] invalid private key length %d", tag, len(privKey))
		return zero, false
	}

	senderAddr, err := DeriveFilAddr(privKey)
	if err != nil {
		log.Printf("[%s] DeriveFilAddr failed: %v", tag, err)
		return zero, false
	}

	EthNoncesMu.Lock()
	nonce, known := EthNonces[senderAddr]
	if !known {
		n, err := node.MpoolGetNonce(ctx, senderAddr)
		if err != nil {
			EthNoncesMu.Unlock()
			log.Printf("[%s] MpoolGetNonce failed: %v", tag, err)
			return zero, false
		}
		nonce = n
	}
	EthNonces[senderAddr] = nonce + 1
	EthNoncesMu.Unlock()

	toEth, err := ethtypes.CastEthAddress(toAddr)
	if err != nil {
		log.Printf("[%s] CastEthAddress failed: %v", tag, err)
		return zero, false
	}

	tx := ethtypes.Eth1559TxArgs{
		ChainID:              31415926,
		Nonce:                int(nonce),
		To:                   &toEth,
		Value:                filbig.Zero(),
		MaxFeePerGas:         types.NanoFil,
		MaxPriorityFeePerGas: filbig.NewInt(100),
		GasLimit:             30_000_000,
		Input:                calldata,
		V:                    filbig.Zero(),
		R:                    filbig.Zero(),
		S:                    filbig.Zero(),
	}

	preimage, err := tx.ToRlpUnsignedMsg()
	if err != nil {
		log.Printf("[%s] ToRlpUnsignedMsg failed: %v", tag, err)
		return zero, false
	}

	sig, err := sigs.Sign(crypto.SigTypeDelegated, privKey, preimage)
	if err != nil {
		log.Printf("[%s] sigs.Sign failed: %v", tag, err)
		return zero, false
	}

	if err := tx.InitialiseSignature(*sig); err != nil {
		log.Printf("[%s] InitialiseSignature failed: %v", tag, err)
		return zero, false
	}

	signed, err := tx.ToRlpSignedMsg()
	if err != nil {
		log.Printf("[%s] ToRlpSignedMsg failed: %v", tag, err)
		return zero, false
	}

	txHash, err := node.EthSendRawTransaction(ctx, signed)
	if err != nil {
		log.Printf("[%s] EthSendRawTransaction failed: %v", tag, err)
		EthNoncesMu.Lock()
		delete(EthNonces, senderAddr)
		EthNoncesMu.Unlock()
		return zero, false
	}

	log.Printf("[%s] tx submitted: from=%s nonce=%d to=%x txHash=%s", tag, senderAddr, nonce, toAddr, txHash)
	return txHash, true
}

// SendEthTx signs and submits an EIP-1559 EVM transaction via EthSendRawTransaction.
// Returns true if the transaction was accepted by the mempool.
func SendEthTx(ctx context.Context, node api.FullNode, privKey []byte, toAddr []byte, calldata []byte, tag string) bool {
	txHash, ok := sendEthTxCore(ctx, node, privKey, toAddr, calldata, tag)
	if !ok {
		return false
	}

	// Best-effort receipt check (non-blocking diagnostic)
	receipt, err := node.EthGetTransactionReceipt(ctx, txHash)
	if err != nil {
		log.Printf("[%s] DIAG: receipt not yet available (pending): %v", tag, err)
	} else if receipt == nil {
		log.Printf("[%s] DIAG: tx %s pending (no receipt yet)", tag, txHash)
	} else {
		log.Printf("[%s] DIAG: tx %s receipt: status=%d gasUsed=%d blockNum=%v", tag, txHash, receipt.Status, receipt.GasUsed, receipt.BlockNumber)
		if receipt.Status == 0 {
			log.Printf("[%s] DIAG: tx %s REVERTED! gasUsed=%d", tag, txHash, receipt.GasUsed)
		}
	}

	return true
}

// SendEthTxConfirmed signs, submits, and waits for an EVM transaction receipt.
// Returns true only if the transaction is mined with status=1 (success).
func SendEthTxConfirmed(ctx context.Context, node api.FullNode, privKey []byte, toAddr []byte, calldata []byte, tag string) bool {
	txHash, ok := sendEthTxCore(ctx, node, privKey, toAddr, calldata, tag)
	if !ok {
		return false
	}

	deadline := time.Now().Add(receiptPollTimeout)
	for time.Now().Before(deadline) {
		receipt, err := node.EthGetTransactionReceipt(ctx, txHash)
		if err != nil || receipt == nil {
			time.Sleep(receiptPollInterval)
			continue
		}

		log.Printf("[%s] tx %s receipt: status=%d gasUsed=%d blockNum=%v", tag, txHash, receipt.Status, receipt.GasUsed, receipt.BlockNumber)
		if receipt.Status == 0 {
			log.Printf("[%s] tx %s REVERTED gasUsed=%d", tag, txHash, receipt.GasUsed)
			return false
		}
		return true
	}

	log.Printf("[%s] tx %s receipt timeout after %v — invalidating nonce cache", tag, txHash, receiptPollTimeout)
	if senderAddr, err := DeriveFilAddr(privKey); err == nil {
		EthNoncesMu.Lock()
		delete(EthNonces, senderAddr)
		EthNoncesMu.Unlock()
	}
	return false
}

// EthCallUint256 performs an eth_call and decodes the returned 32-byte uint256.
func EthCallUint256(ctx context.Context, node api.FullNode, to []byte, calldata []byte) (*big.Int, error) {
	toEth, err := ethtypes.CastEthAddress(to)
	if err != nil {
		return nil, err
	}
	result, err := node.EthCall(ctx, ethtypes.EthCall{
		To:   &toEth,
		Data: ethtypes.EthBytes(calldata),
	}, ethtypes.NewEthBlockNumberOrHashFromPredefined("latest"))
	if err != nil {
		return nil, err
	}
	if len(result) < 32 {
		return big.NewInt(0), nil
	}
	return new(big.Int).SetBytes(result[len(result)-32:]), nil
}

// EthCallBool performs an eth_call and decodes the returned value as bool.
func EthCallBool(ctx context.Context, node api.FullNode, to []byte, calldata []byte) (bool, error) {
	n, err := EthCallUint256(ctx, node, to, calldata)
	if err != nil {
		return false, err
	}
	return n.Sign() != 0, nil
}

// EthCallRaw performs an eth_call and returns the raw byte result.
func EthCallRaw(ctx context.Context, node api.FullNode, to []byte, calldata []byte) ([]byte, error) {
	toEth, err := ethtypes.CastEthAddress(to)
	if err != nil {
		return nil, err
	}
	result, err := node.EthCall(ctx, ethtypes.EthCall{
		To:   &toEth,
		Data: ethtypes.EthBytes(calldata),
	}, ethtypes.NewEthBlockNumberOrHashFromPredefined("latest"))
	if err != nil {
		return nil, err
	}
	return []byte(result), nil
}

// ReadAccountFunds reads the `funds` field from FilecoinPay's accounts(token, owner).
// The function returns a 4-tuple; funds is the first uint256.
func ReadAccountFunds(ctx context.Context, node api.FullNode, filPayAddr, tokenAddr, ownerAddr []byte) *big.Int {
	calldata := BuildCalldata(SigAccounts, EncodeAddress(tokenAddr), EncodeAddress(ownerAddr))
	result, err := EthCallRaw(ctx, node, filPayAddr, calldata)
	if err != nil {
		log.Printf("[foc] ReadAccountFunds failed: %v", err)
		return big.NewInt(0)
	}
	if len(result) < 32 {
		return big.NewInt(0)
	}
	return new(big.Int).SetBytes(result[:32])
}

// ReadAccountLockup reads the `lockup` field from FilecoinPay's accounts(token, owner).
// The function returns a 4-tuple; lockup is the second uint256 (bytes 32-64).
func ReadAccountLockup(ctx context.Context, node api.FullNode, filPayAddr, tokenAddr, ownerAddr []byte) *big.Int {
	calldata := BuildCalldata(SigAccounts, EncodeAddress(tokenAddr), EncodeAddress(ownerAddr))
	result, err := EthCallRaw(ctx, node, filPayAddr, calldata)
	if err != nil {
		log.Printf("[foc] ReadAccountLockup failed: %v", err)
		return big.NewInt(0)
	}
	if len(result) < 64 {
		return big.NewInt(0)
	}
	return new(big.Int).SetBytes(result[32:64])
}

// ReadRailPaymentRate calls getRail(railId) on FilecoinPay and returns the paymentRate field.
// getRail returns a 12-field tuple; paymentRate is at word index 5 (bytes 160-192).
func ReadRailPaymentRate(ctx context.Context, node api.FullNode, filPayAddr []byte, railID uint64) *big.Int {
	calldata := BuildCalldata(SigGetRail, EncodeBigInt(new(big.Int).SetUint64(railID)))
	result, err := EthCallRaw(ctx, node, filPayAddr, calldata)
	if err != nil {
		log.Printf("[foc] ReadRailPaymentRate(%d) failed: %v", railID, err)
		return nil
	}
	if len(result) < 192 { // need at least 6 words
		return nil
	}
	return new(big.Int).SetBytes(result[160:192])
}

// EncodeBigInt ABI-encodes a *big.Int as a 32-byte big-endian uint256.
func EncodeBigInt(n *big.Int) []byte {
	buf := make([]byte, 32)
	if n != nil {
		b := n.Bytes()
		if len(b) <= 32 {
			copy(buf[32-len(b):], b)
		}
	}
	return buf
}

// EncodeBool ABI-encodes a bool as a 32-byte value (0 or 1).
func EncodeBool(b bool) []byte {
	buf := make([]byte, 32)
	if b {
		buf[31] = 1
	}
	return buf
}

// EncodeAddress ABI-encodes an Ethereum-style address as a 32-byte padded value.
func EncodeAddress(addr []byte) []byte {
	buf := make([]byte, 32)
	if len(addr) >= 20 {
		copy(buf[12:], addr[:20])
	}
	return buf
}

// EncodeUint256 ABI-encodes a uint64 as a 32-byte big-endian uint256.
func EncodeUint256(n uint64) []byte {
	buf := make([]byte, 32)
	buf[24] = byte(n >> 56)
	buf[25] = byte(n >> 48)
	buf[26] = byte(n >> 40)
	buf[27] = byte(n >> 32)
	buf[28] = byte(n >> 24)
	buf[29] = byte(n >> 16)
	buf[30] = byte(n >> 8)
	buf[31] = byte(n)
	return buf
}
