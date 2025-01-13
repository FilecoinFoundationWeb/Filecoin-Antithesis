package resources

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"os"

	"github.com/antithesishq/antithesis-sdk-go/assert"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/builtin"
	"github.com/filecoin-project/go-state-types/builtin/v10/eam"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/types/ethtypes"
	cbg "github.com/whyrusleeping/cbor-gen"
	"golang.org/x/crypto/sha3"
)

func DeploySmartContracts(ctx context.Context, api api.FullNode, contractPath string, fundingAmount abi.TokenAmount) (address.Address, address.Address, error) {
	genesisWallet, err := GetGenesisWallet(ctx, api)
	assert.Always(err == nil, "Retrieve the genesis wallet", map[string]interface{}{"error": err})

	// Create a delegated wallet
	delegatedWallet, err := CreateWallet(ctx, api, types.KTDelegated)
	assert.Always(err == nil, "Create a delegated wallet", map[string]interface{}{"error": err})

	// Fund the delegated wallet
	err = SendFunds(ctx, api, genesisWallet, delegatedWallet, fundingAmount)
	assert.Always(err == nil, "Fund the delegated wallet", map[string]interface{}{"genesisWallet": genesisWallet, "delegatedWallet": delegatedWallet, "fundingAmount": fundingAmount})

	// Read and decode contract
	contractHex, err := os.ReadFile(contractPath)
	assert.Always(err == nil, "Read smart contract file", map[string]interface{}{"filePath": contractPath, "error": err})

	contract, err := hex.DecodeString(string(contractHex))
	assert.Always(err == nil, "Decode smart contract", map[string]interface{}{"error": err})

	initcode := abi.CborBytes(contract)
	params, err := actors.SerializeParams(&initcode)
	assert.Always(err == nil, "Serialize contract parameters", map[string]interface{}{"error": err})

	msg := &types.Message{
		To:     builtin.EthereumAddressManagerActorAddr,
		From:   delegatedWallet,
		Value:  abi.NewTokenAmount(0),
		Method: builtin.MethodsEAM.CreateExternal,
		Params: params,
	}

	log.Println("Sending deployment message")
	smsg, err := api.MpoolPushMessage(ctx, msg, nil)
	assert.Always(err == nil, "Failed to push smart contract deployment message to mempool", map[string]interface{}{"error": err})

	wait, err := api.StateWaitMsg(ctx, smsg.Cid(), 5, 100, false)
	if err != nil || !wait.Receipt.ExitCode.IsSuccess() {
		log.Println("Message didn't land, attempting replay...")
		result, replayErr := api.StateReplay(ctx, types.EmptyTSK, smsg.Cid())
		assert.Always(replayErr == nil, "Replay deployment message", map[string]interface{}{"error": replayErr})
		return delegatedWallet, address.Undef, fmt.Errorf("deployment failed: %v", result.Error)
	}

	var result eam.CreateReturn
	err = result.UnmarshalCBOR(bytes.NewReader(wait.Receipt.Return))
	assert.Always(err == nil, "Unmarshal deployment result", map[string]interface{}{"error": err})

	// Convert deployed EthAddress to Filecoin address
	castEthAddr, err := ethtypes.CastEthAddress(result.EthAddress[:])
	assert.Always(err == nil, "Cast deployed EthAddress to valid EthAddress", map[string]interface{}{"error": err})

	deployedAddr, err := castEthAddr.ToFilecoinAddress()
	assert.Always(err == nil, "Convert cast EthAddress to Filecoin address", map[string]interface{}{"error": err})

	return delegatedWallet, deployedAddr, nil
}

// InvokeContract invokes a smart contract and ensures the message lands on chain.
func InvokeContract(ctx context.Context, api api.FullNode, from address.Address, contract address.Address, funcName string, inputData []byte) ([]byte, error) {
	entryPoint := CalcFuncSignature(funcName)

	// Prepare the parameters for the message
	params := append(entryPoint, inputData...)
	var cborBuffer bytes.Buffer
	err := cbg.WriteByteArray(&cborBuffer, params) // Serialize into CBOR
	if err != nil {
		return nil, fmt.Errorf("failed to serialize parameters into CBOR: %w", err)
	}

	msg := &types.Message{
		To:       contract,
		From:     from,
		Value:    abi.NewTokenAmount(0),
		Method:   builtin.MethodsEVM.InvokeContract,
		GasLimit: 1000000000,
		Params:   cborBuffer.Bytes(),
	}

	log.Println("Sending invocation message")
	smsg, err := api.MpoolPushMessage(ctx, msg, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to push message to mempool: %w", err)
	}

	wait, err := api.StateWaitMsg(ctx, smsg.Cid(), 5, 100, false)
	if err != nil || !wait.Receipt.ExitCode.IsSuccess() {
		// Replay to ensure the message lands on the chain
		result, replayErr := api.StateReplay(ctx, types.EmptyTSK, smsg.Cid())
		if replayErr != nil {
			return nil, fmt.Errorf("replay failed: %w", replayErr)
		}
		return nil, fmt.Errorf("smart contract invocation failed: %v", result.Error)
	}

	return wait.Receipt.Return, nil
}

// CalcFuncSignature calculates a function's signature hash.
func CalcFuncSignature(funcName string) []byte {
	hash := sha3.Sum256([]byte(funcName))
	return hash[:4]
}

func GenerateInputData(ctx context.Context, api api.FullNode, from address.Address) []byte {
	// Lookup the ID address for the provided "from" address
	fromId, err := api.StateLookupID(ctx, from, types.EmptyTSK)
	assert.Always(err == nil, "StateLookupID failed for 'from' address", map[string]interface{}{"error": err, "from": from})

	// Convert the ID address to an Ethereum-compatible address
	senderEthAddr, err := ethtypes.EthAddressFromFilecoinAddress(fromId)
	assert.Always(err == nil, "Failed to convert Filecoin address to Ethereum address", map[string]interface{}{"error": err, "fromId": fromId})

	// Prepare input data with the Ethereum address
	inputData := make([]byte, 32)
	copy(inputData[32-len(senderEthAddr):], senderEthAddr[:])

	return inputData
}
