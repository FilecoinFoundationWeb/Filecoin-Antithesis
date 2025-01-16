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

	log.Println("Sending create message")
	smsg, err := api.MpoolPushMessage(ctx, msg, nil)
	assert.Always(err == nil, "Push a create contract message", map[string]interface{}{"error": err})
	time.Sleep(5 * time.Second)
	log.Println("Waiting for message to execute")
	wait, err := api.StateWaitMsg(ctx, smsg.Cid(), 5, 100, false)
	assert.Always(err == nil, "Wait for message to execute", map[string]interface{}{"Cid": smsg.Cid(), "error": err})
	assert.Always(wait.Receipt.ExitCode.IsSuccess(), "Contract installation failed", map[string]interface{}{"ExitCode": wait.Receipt.ExitCode})

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
	log.Printf("InvokeSolidity: Preparing to invoke contract from sender: %s to target: %s", sender, target)
	log.Printf("Selector: %x, InputData: %x", selector, inputData)
	return InvokeSolidityWithValue(ctx, api, sender, target, selector, inputData, big.Zero())
}

// InvokeContractByFuncName invokes a smart contract function using its signature and input data.
func InvokeContractByFuncName(ctx context.Context, api api.FullNode, fromAddr address.Address, idAddr address.Address, funcSignature string, inputData []byte) ([]byte, *api.MsgLookup, error) {
	log.Printf("InvokeContractByFuncName: Preparing to invoke function '%s' on contract at address: %s from sender: %s", funcSignature, idAddr, fromAddr)
	entryPoint := CalcFuncSignature(funcSignature)
	log.Printf("Function signature hash (entry point): %x", entryPoint)

	// Invoke Solidity function
	wait, err := InvokeSolidity(ctx, api, fromAddr, idAddr, entryPoint, inputData)
	log.Printf("InvokeSolidity completed. MsgLookup: %v, Error: %v", wait, err)
	if err != nil {
		return nil, wait, fmt.Errorf("failed to invoke Solidity function: %w", err)
	}

	// Check for successful execution
	if !wait.Receipt.ExitCode.IsSuccess() {
		log.Printf("InvokeContractByFuncName: Contract execution failed. ExitCode: %d", wait.Receipt.ExitCode)
		replayResult, replayErr := api.StateReplay(ctx, types.EmptyTSK, wait.Message)
		if replayErr != nil {
			log.Printf("StateReplay failed. Error: %v", replayErr)
			return nil, wait, fmt.Errorf("failed to replay failed message: %w", replayErr)
		}
		log.Printf("StateReplay completed. Error: %s", replayResult.Error)
		return nil, wait, fmt.Errorf("invoke failed with error: %v", replayResult.Error)
	}

	// Parse the result
	result, err := cbg.ReadByteArray(bytes.NewBuffer(wait.Receipt.Return), uint64(len(wait.Receipt.Return)))
	log.Printf("Parsed return data: %x, Error: %v", result, err)
	if err != nil {
		return nil, wait, fmt.Errorf("failed to read return data: %w", err)
	}

	log.Printf("InvokeContractByFuncName: Successfully invoked function '%s'. Result: %x", funcSignature, result)
	return result, wait, nil
}

func InvokeSolidityWithValue(ctx context.Context, api api.FullNode, sender address.Address, target address.Address, selector []byte, inputData []byte, value big.Int) (*api.MsgLookup, error) {
	log.Printf("InvokeSolidityWithValue: Preparing to invoke contract from sender: %s to target: %s with value: %s", sender, target, value.String())
	log.Printf("Selector: %x, InputData: %x", selector, inputData)

	params := append(selector, inputData...)
	log.Printf("Serialized parameters for invocation: %x", params)

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

	log.Println("Sending invoke message to the mempool")
	smsg, err := api.MpoolPushMessage(ctx, msg, nil)
	time.Sleep(5 * time.Second)
	if err != nil {
		log.Printf("Failed to push message to mempool. Error: %v", err)
		return nil, err
	}
	log.Printf("Message pushed to mempool. CID: %s", smsg.Cid())

	log.Println("Waiting for the message to execute")
	wait, err := api.StateWaitMsg(ctx, smsg.Cid(), 5, 100, false)
	log.Printf("StateWaitMsg completed. MsgLookup: %v, Error: %v", wait, err)
	if err != nil {
		return nil, err
	}
	log.Printf("Message execution result: %v", wait.Receipt)
	log.Printf("Message execution result: %v", wait.Receipt.ExitCode)
	if !wait.Receipt.ExitCode.IsSuccess() {
		log.Printf("Contract execution failed. ExitCode: %d", wait.Receipt.ExitCode)
		result, err := api.StateReplay(ctx, types.EmptyTSK, wait.Message)
		log.Printf("StateReplay completed. Error: %v", err)
		log.Printf("StateReplay result: %v", result)
		if err != nil {
			return nil, fmt.Errorf("replay failed: %v", err)
		}
		return nil, fmt.Errorf("invoke failed with error: %v", result.Error)
	}

	log.Println("InvokeSolidityWithValue: Successfully executed the invoke message")
	return wait, nil
}

// Utility function to calculate the function signature hash
func CalcFuncSignature(funcName string) []byte {
	hasher := sha3.NewLegacyKeccak256()
	hasher.Write([]byte(funcName))
	hash := hasher.Sum(nil)
	return hash[:4]
}

func InputDataFromFrom(ctx context.Context, api api.FullNode, from address.Address) []byte {
	fromId, err := api.StateLookupID(ctx, from, types.EmptyTSK)
	assert.Always(err == nil, "Failed to lookup ID address", map[string]interface{}{"error": err})
	senderEthAddr, err := ethtypes.EthAddressFromFilecoinAddress(fromId)
	assert.Always(err == nil, "Failed to convert Filecoin address to Ethereum address", map[string]interface{}{"error": err})
	inputData := make([]byte, 32)
	copy(inputData[32-len(senderEthAddr):], senderEthAddr[:])
	return inputData
}
