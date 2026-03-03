package main

import (
	"log"
	"math/big"
	"sync"

	"workload/internal/foc"

	"github.com/antithesishq/antithesis-sdk-go/assert"
	"github.com/antithesishq/antithesis-sdk-go/random"
)

// ===========================================================================
// FWSS Lifecycle Vectors
//
// These vectors exercise the FilecoinWarmStorageService (FWSS) contract stack
// end-to-end. Each vector uses assert.Sometimes because any transaction can
// fail under Antithesis fault injection.
//
//   DoFWSSDeposit          — USDFC approve + deposit into FilecoinPay
//   DoFWSSApproveOperator  — setOperatorApproval for FWSS on FilecoinPay
//   DoFWSSCreateDataSet    — EIP-712 signed dataset creation via PDPVerifier
// ===========================================================================

const focUSDFCUnit = 1e18

// fwssState holds runtime state shared between FWSS vectors.
// Not part of foc.Config because it's stress-engine specific.
var (
	fwssState   fwssRuntime
	fwssStateMu sync.Mutex
)

type fwssRuntime struct {
	Deposited        bool     // at least one successful deposit
	OperatorApproved bool     // at least one successful operator approval
	ActiveDataSetID  *big.Int // cached from last successful createDataSet
}

// DoFWSSDeposit approves FilecoinPay as a USDFC spender and deposits funds into
// the client's FilecoinPay account.
func DoFWSSDeposit() {
	if focCfg == nil || focCfg.ClientKey == nil ||
		focCfg.USDFCAddr == nil || focCfg.FilPayAddr == nil {
		return
	}

	_, node := pickNode()

	// Deposit 1–10 USDFC
	amount := new(big.Int).Mul(
		big.NewInt(int64(rngIntn(10)+1)),
		big.NewInt(focUSDFCUnit),
	)

	// Step 1: approve(FilecoinPay, amount) on USDFC
	approveData := append(append([]byte{}, foc.SigApprove...),
		foc.EncodeAddress(focCfg.FilPayAddr)...,
	)
	approveData = append(approveData, foc.EncodeBigInt(amount)...)
	if !foc.SendEthTx(ctx, node, focCfg.ClientKey, focCfg.USDFCAddr, approveData, "fwss-approve") {
		return
	}

	// Step 2: deposit(usdfc, clientEthAddr, amount) on FilecoinPay
	depositData := append(append([]byte{}, foc.SigDeposit...),
		foc.EncodeAddress(focCfg.USDFCAddr)...,
	)
	depositData = append(depositData, foc.EncodeAddress(focCfg.ClientEthAddr)...)
	depositData = append(depositData, foc.EncodeBigInt(amount)...)
	ok := foc.SendEthTx(ctx, node, focCfg.ClientKey, focCfg.FilPayAddr, depositData, "fwss-deposit")

	log.Printf("[fwss-deposit] amount=%s ok=%v", amount, ok)

	assert.Sometimes(ok, "USDFC deposit into FilecoinPay succeeds", map[string]any{
		"amount": amount.String(),
	})

	if ok {
		fwssStateMu.Lock()
		fwssState.Deposited = true
		fwssStateMu.Unlock()
	}
}

// DoFWSSApproveOperator grants the FWSS contract operator rights on the client's
// FilecoinPay account, allowing FWSS to create and manage payment rails.
func DoFWSSApproveOperator() {
	if focCfg == nil || focCfg.ClientKey == nil ||
		focCfg.FilPayAddr == nil || focCfg.FWSSAddr == nil || focCfg.USDFCAddr == nil {
		return
	}

	fwssStateMu.Lock()
	deposited := fwssState.Deposited
	fwssStateMu.Unlock()
	if !deposited {
		return // need a deposit first
	}

	_, node := pickNode()

	// setOperatorApproval(operator, token, approved, lockupFixedRate, lockupPeriod, lockupFlexRate)
	// Approve FWSS as operator for USDFC with generous lockup params.
	lockupFixedRate := new(big.Int).Mul(big.NewInt(100), big.NewInt(focUSDFCUnit)) // 100 USDFC/epoch
	lockupPeriod := big.NewInt(2880)                                               // ~1 day in epochs
	lockupFlexRate := new(big.Int).Mul(big.NewInt(10), big.NewInt(focUSDFCUnit))   // 10 USDFC/epoch

	calldata := append(append([]byte{}, foc.SigSetOpApproval...),
		foc.EncodeAddress(focCfg.FWSSAddr)...,
	)
	calldata = append(calldata, foc.EncodeAddress(focCfg.USDFCAddr)...)
	calldata = append(calldata, foc.EncodeBool(true)...)
	calldata = append(calldata, foc.EncodeBigInt(lockupFixedRate)...)
	calldata = append(calldata, foc.EncodeBigInt(lockupPeriod)...)
	calldata = append(calldata, foc.EncodeBigInt(lockupFlexRate)...)

	ok := foc.SendEthTx(ctx, node, focCfg.ClientKey, focCfg.FilPayAddr, calldata, "fwss-approve-op")

	log.Printf("[fwss-approve-op] ok=%v", ok)

	assert.Sometimes(ok, "FWSS operator approval succeeds", nil)

	if ok {
		fwssStateMu.Lock()
		fwssState.OperatorApproved = true
		fwssStateMu.Unlock()
	}
}

// DoFWSSCreateDataSet creates a new dataset via PDPVerifier.createDataSet(fwssAddr, extraData).
// This triggers the FWSS.dataSetCreated() callback, which sets up the dataset, payment rail, etc.
// Requires: deposit done, operator approved, SP registered.
func DoFWSSCreateDataSet() {
	if focCfg == nil {
		return
	}
	if focCfg.ClientKey == nil || focCfg.SPKey == nil ||
		focCfg.FWSSAddr == nil || focCfg.PDPAddr == nil || focCfg.SPEthAddr == nil {
		log.Printf("[fwss-create-ds] skipped: missing config (clientKey=%v spKey=%v fwss=%v pdp=%v spAddr=%v)",
			focCfg.ClientKey != nil, focCfg.SPKey != nil,
			focCfg.FWSSAddr != nil, focCfg.PDPAddr != nil, focCfg.SPEthAddr != nil)
		return
	}

	fwssStateMu.Lock()
	ready := fwssState.Deposited && fwssState.OperatorApproved
	fwssStateMu.Unlock()
	if !ready {
		return // need deposit + operator approval first
	}

	_, node := pickNode()

	// Generate a unique clientDataSetId
	clientDataSetId := new(big.Int).SetUint64(random.GetRandom())

	// Metadata keys/values (minimal — just enough to satisfy the contract)
	metadataKeys := []string{"source"}
	metadataValues := []string{"antithesis-stress"}

	// The payee is the SP's eth address
	payee := focCfg.SPEthAddr

	// Sign EIP-712 CreateDataSet with client key (payer)
	sig, err := foc.SignEIP712CreateDataSet(
		focCfg.ClientKey, focCfg.FWSSAddr,
		clientDataSetId, payee,
		metadataKeys, metadataValues,
	)
	if err != nil {
		log.Printf("[fwss-create-ds] EIP-712 signing failed: %v", err)
		return
	}

	// ABI-encode the extraData for FWSS.dataSetCreated():
	//   abi.encode(payer, clientDataSetId, payee, metadataKeys, metadataValues, signature)
	// This is a dynamic encoding with offsets.
	extraData := encodeCreateDataSetExtra(focCfg.ClientEthAddr, clientDataSetId, payee, metadataKeys, metadataValues, sig)

	// Build PDPVerifier.createDataSet(fwssAddr, extraData) calldata
	calldata := buildCreateDataSetCalldata(focCfg.FWSSAddr, extraData)

	// SP calls createDataSet (SP is the msg.sender for PDPVerifier)
	ok := foc.SendEthTx(ctx, node, focCfg.SPKey, focCfg.PDPAddr, calldata, "fwss-create-ds")

	log.Printf("[fwss-create-ds] clientDataSetId=%s ok=%v", clientDataSetId, ok)

	assert.Sometimes(ok, "FWSS dataset creation completes end-to-end", map[string]any{
		"clientDataSetId": clientDataSetId.String(),
	})

	if ok {
		fwssStateMu.Lock()
		fwssState.ActiveDataSetID = clientDataSetId
		fwssStateMu.Unlock()
	}
}

// encodeCreateDataSetExtra ABI-encodes the extraData for FWSS.dataSetCreated().
// Layout: payer(address) | clientDataSetId(uint256) | payee(address) |
//
//	offset(metadataKeys) | offset(metadataValues) | offset(signature) |
//	metadataKeys(string[]) | metadataValues(string[]) | signature(bytes)
func encodeCreateDataSetExtra(payer []byte, clientDataSetId *big.Int, payee []byte, keys, values []string, sig []byte) []byte {
	// Head section: 6 slots (payer, clientDataSetId, payee, 3 offsets)
	headSlots := 6
	headSize := headSlots * 32

	// Encode dynamic arrays
	keysEncoded := encodeStringArray(keys)
	valuesEncoded := encodeStringArray(values)
	sigEncoded := encodeBytes(sig)

	// Offsets (relative to start of data area, which begins after head)
	keysOffset := big.NewInt(int64(headSize))
	valuesOffset := big.NewInt(int64(headSize + len(keysEncoded)))
	sigOffset := big.NewInt(int64(headSize + len(keysEncoded) + len(valuesEncoded)))

	var buf []byte
	buf = append(buf, foc.EncodeAddress(payer)...)
	buf = append(buf, foc.EncodeBigInt(clientDataSetId)...)
	buf = append(buf, foc.EncodeAddress(payee)...)
	buf = append(buf, foc.EncodeBigInt(keysOffset)...)
	buf = append(buf, foc.EncodeBigInt(valuesOffset)...)
	buf = append(buf, foc.EncodeBigInt(sigOffset)...)
	buf = append(buf, keysEncoded...)
	buf = append(buf, valuesEncoded...)
	buf = append(buf, sigEncoded...)

	return buf
}

// encodeStringArray ABI-encodes a string[] for the dynamic section.
func encodeStringArray(strs []string) []byte {
	n := len(strs)
	// Length prefix
	var buf []byte
	buf = append(buf, foc.EncodeBigInt(big.NewInt(int64(n)))...)

	// Offsets for each string (relative to start of string data after offsets)
	offsetBase := n * 32 // bytes of offset slots
	offsets := make([]int, n)
	currentOffset := offsetBase
	for i, s := range strs {
		offsets[i] = currentOffset
		// Each string: 32 bytes length + padded data
		currentOffset += 32 + padTo32(len(s))
	}
	for _, off := range offsets {
		buf = append(buf, foc.EncodeBigInt(big.NewInt(int64(off)))...)
	}

	// String data
	for _, s := range strs {
		buf = append(buf, foc.EncodeBigInt(big.NewInt(int64(len(s))))...)
		data := []byte(s)
		buf = append(buf, data...)
		// Pad to 32 bytes
		if pad := padTo32(len(data)) - len(data); pad > 0 {
			buf = append(buf, make([]byte, pad)...)
		}
	}

	return buf
}

// encodeBytes ABI-encodes bytes for the dynamic section.
func encodeBytes(data []byte) []byte {
	var buf []byte
	buf = append(buf, foc.EncodeBigInt(big.NewInt(int64(len(data))))...)
	buf = append(buf, data...)
	if pad := padTo32(len(data)) - len(data); pad > 0 {
		buf = append(buf, make([]byte, pad)...)
	}
	return buf
}

// padTo32 rounds up to the nearest multiple of 32.
func padTo32(n int) int {
	if n == 0 {
		return 0
	}
	return ((n + 31) / 32) * 32
}

// buildCreateDataSetCalldata builds the full calldata for
// PDPVerifier.createDataSet(address listenerAddr, bytes calldata extraData).
func buildCreateDataSetCalldata(fwssAddr []byte, extraData []byte) []byte {
	var buf []byte
	buf = append(buf, foc.SigCreateDataSet...)
	buf = append(buf, foc.EncodeAddress(fwssAddr)...)
	// Offset to extraData (starts at byte 64 = 2 slots after selector area)
	buf = append(buf, foc.EncodeBigInt(big.NewInt(64))...)
	// extraData as bytes: length + padded data
	buf = append(buf, encodeBytes(extraData)...)
	return buf
}
