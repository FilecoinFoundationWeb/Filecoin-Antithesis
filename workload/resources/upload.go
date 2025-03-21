package resources

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/antithesishq/antithesis-sdk-go/assert"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/builtin"
	"github.com/filecoin-project/go-state-types/builtin/v10/eam"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build/buildconstants"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/types/ethtypes"
	cbg "github.com/whyrusleeping/cbor-gen"
	"golang.org/x/crypto/sha3"
)

func DeployContractWithValue(ctx context.Context, api api.FullNode, sender address.Address, bytecode []byte, value big.Int) eam.CreateReturn {
	method := builtin.MethodsEAM.CreateExternal
	initcode := abi.CborBytes(bytecode)

	params, Actorerr := actors.SerializeParams(&initcode)
	assert.Always(Actorerr == nil, "Serialize contract initialization parameters", map[string]interface{}{"error": Actorerr})

	msg := &types.Message{
		To:     builtin.EthereumAddressManagerActorAddr,
		From:   sender,
		Value:  value,
		Method: method,
		Params: params,
	}

	smsg, err := api.MpoolPushMessage(ctx, msg, nil)
	assert.Always(err == nil, "Push a create contract message", map[string]interface{}{"error": err})
	time.Sleep(5 * time.Second)

	wait, err := api.StateWaitMsg(ctx, smsg.Cid(), 5, 100, false)
	assert.Sometimes(err == nil, "Wait for message to execute", map[string]interface{}{"Cid": smsg.Cid(), "error": err})
	assert.Sometimes(wait.Receipt.ExitCode.IsSuccess(), "Contract installation failed", map[string]interface{}{"ExitCode": wait.Receipt.ExitCode})

	var result eam.CreateReturn
	r := bytes.NewReader(wait.Receipt.Return)
	err = result.UnmarshalCBOR(r)
	assert.Always(err == nil, "Unmarshal CBOR result", map[string]interface{}{"error": err})

	return result
}

func DeployContract(ctx context.Context, api api.FullNode, sender address.Address, bytecode []byte) eam.CreateReturn {
	return DeployContractWithValue(ctx, api, sender, bytecode, big.Zero())
}

func DeployContractFromFilenameWithValue(ctx context.Context, api api.FullNode, binFilename string, value big.Int) (address.Address, address.Address) {
	contractHex, err := os.ReadFile(binFilename)
	assert.Always(err == nil, "Read smart contract file", map[string]interface{}{"filePath": binFilename, "error": err})

	contractHex = bytes.TrimRight(contractHex, "\n")
	contract, err := hex.DecodeString(string(contractHex))
	assert.Always(err == nil, "Decode smart contract hex string", map[string]interface{}{"error": err})

	fromAddr, err := api.WalletDefaultAddress(ctx)
	assert.Always(err == nil, "Retrieve default wallet address", map[string]interface{}{"error": err})

	result := DeployContractWithValue(ctx, api, fromAddr, contract, value)

	idAddr, err := address.NewIDAddress(result.ActorID)
	assert.Always(err == nil, "Create ID address from ActorID", map[string]interface{}{"error": err})
	return fromAddr, idAddr
}

func DeployContractFromFilename(ctx context.Context, api api.FullNode, binFilename string) (address.Address, address.Address) {
	return DeployContractFromFilenameWithValue(ctx, api, binFilename, big.Zero())
}

func InvokeSolidity(ctx context.Context, api api.FullNode, sender address.Address, target address.Address, selector []byte, inputData []byte) (*api.MsgLookup, error) {
	return InvokeSolidityWithValue(ctx, api, sender, target, selector, inputData, big.Zero())
}

func InvokeContractByFuncName(ctx context.Context, api api.FullNode, fromAddr address.Address, idAddr address.Address, funcSignature string, inputData []byte) ([]byte, *api.MsgLookup, error) {
	entryPoint := CalcFuncSignature(funcSignature)

	wait, err := InvokeSolidity(ctx, api, fromAddr, idAddr, entryPoint, inputData)
	assert.Sometimes(err == nil, "Invoke Solidity function", map[string]interface{}{"error": err})
	if err != nil {
		return nil, wait, fmt.Errorf("failed to invoke Solidity function: %w", err)
	}

	assert.Sometimes(wait.Receipt.ExitCode.IsSuccess(), "Check if contract execution succeeded", map[string]interface{}{"ExitCode": wait.Receipt.ExitCode})
	if !wait.Receipt.ExitCode.IsSuccess() {
		replayResult, replayErr := api.StateReplay(ctx, types.EmptyTSK, wait.Message)
		assert.Sometimes(replayErr == nil, "Replay failed message", map[string]interface{}{"error": replayErr})
		if replayErr != nil {
			return nil, wait, fmt.Errorf("failed to replay failed message: %w", replayErr)
		}
		return nil, wait, fmt.Errorf("invoke failed with error: %v", replayResult.Error)
	}

	result, err := cbg.ReadByteArray(bytes.NewBuffer(wait.Receipt.Return), uint64(len(wait.Receipt.Return)))
	assert.Sometimes(err == nil, "Read return data from contract execution", map[string]interface{}{"error": err})
	return result, wait, err
}

func InvokeSolidityWithValue(ctx context.Context, api api.FullNode, sender address.Address, target address.Address, selector []byte, inputData []byte, value big.Int) (*api.MsgLookup, error) {
	params := append(selector, inputData...)
	var buffer bytes.Buffer
	err := cbg.WriteByteArray(&buffer, params)
	assert.Always(err == nil, "Write byte array to buffer", map[string]interface{}{"error": err})

	params = buffer.Bytes()

	msg := &types.Message{
		To:       target,
		From:     sender,
		Value:    value,
		Method:   builtin.MethodsEVM.InvokeContract,
		GasLimit: buildconstants.BlockGasLimit,
		Params:   params,
	}

	smsg, err := api.MpoolPushMessage(ctx, msg, nil)
	assert.Sometimes(err == nil, "Push message to invoke contract", map[string]interface{}{"error": err})
	time.Sleep(5 * time.Second)
	if err != nil {
		return nil, err
	}
	time.Sleep(5 * time.Second)
	wait, err := api.StateWaitMsg(ctx, smsg.Cid(), 5, 100, false)
	assert.Sometimes(err == nil, "Wait for invoke message to execute", map[string]interface{}{"Cid": smsg.Cid(), "error": err})

	if !wait.Receipt.ExitCode.IsSuccess() {
		log.Print("We are here!")
		replayResult, err := api.StateReplay(ctx, types.EmptyTSK, wait.Message)
		log.Printf("StateReplay Error (err): %s", err)
		log.Printf("StateReplay Error (replayResult.Error): %s", replayResult.Error)
		assert.Sometimes(err == nil, "Replay failed invoke message", map[string]interface{}{"error": err})
		return nil, fmt.Errorf("invoke failed with error: %v", replayResult.Error)
	}
	return wait, nil
}

func CalcFuncSignature(funcName string) []byte {
	hasher := sha3.NewLegacyKeccak256()
	hasher.Write([]byte(funcName))
	return hasher.Sum(nil)[:4]
}

func InputDataFromFrom(ctx context.Context, api api.FullNode, from address.Address) []byte {
	fromID, err := api.StateLookupID(ctx, from, types.EmptyTSK)
	assert.Always(err == nil, "Lookup ID address for sender", map[string]interface{}{"error": err})

	senderEthAddr, err := ethtypes.EthAddressFromFilecoinAddress(fromID)
	assert.Always(err == nil, "Convert Filecoin address to Ethereum address", map[string]interface{}{"error": err})

	inputData := make([]byte, 32)
	copy(inputData[32-len(senderEthAddr):], senderEthAddr[:])
	return inputData
}
