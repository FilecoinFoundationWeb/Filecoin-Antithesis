package resources

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"

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
	"github.com/filecoin-project/lotus/chain/wallet/key"
	cbg "github.com/whyrusleeping/cbor-gen"
	"golang.org/x/crypto/sha3"
)

func DeployContractFromFilenameWithValue(ctx context.Context, api api.FullNode, binFilename string, value big.Int) (address.Address, address.Address) {
	contractHex, err := os.ReadFile(binFilename)
	if err != nil {
		log.Fatalf("Failed to read contract file: %v", err)
	}
	// strip any trailing newlines from the file
	contractHex = bytes.TrimRight(contractHex, "\n")

	contract, err := hex.DecodeString(string(contractHex))

	fromAddr, err := api.WalletDefaultAddress(ctx)

	result := DeployContractWithValue(ctx, api, fromAddr, contract, value)

	idAddr, err := address.NewIDAddress(result.ActorID)
	return fromAddr, idAddr
}

func DeployContractWithValue(ctx context.Context, api api.FullNode, sender address.Address, bytecode []byte, value big.Int) eam.CreateReturn {

	method := builtin.MethodsEAM.CreateExternal
	initcode := abi.CborBytes(bytecode)
	params, errActors := actors.SerializeParams(&initcode)
	if errActors != nil {
		log.Fatalf("Failed to serialize params: %v", errActors)
	}
	msg := &types.Message{
		To:     builtin.EthereumAddressManagerActorAddr,
		From:   sender,
		Value:  value,
		Method: method,
		Params: params,
	}

	log.Println("sending create message")
	smsg, err := api.MpoolPushMessage(ctx, msg, nil)
	if err != nil {
		log.Fatalf("Failed to push message to mempool: %v", err)
	}

	log.Println("waiting for message to execute")
	wait, err := api.StateWaitMsg(ctx, smsg.Cid(), 3, 0, false)
	if err != nil {
		log.Fatalf("Failed to wait for message execution: %v", err)
	}

	if !wait.Receipt.ExitCode.IsSuccess() {
		log.Fatalf("Contract installation failed with exit code: %v", wait.Receipt.ExitCode)
	}

	var result eam.CreateReturn
	r := bytes.NewReader(wait.Receipt.Return)
	err = result.UnmarshalCBOR(r)
	if err != nil {
		log.Fatalf("Failed to unmarshal CBOR: %v", err)
	}

	return result
}

func DeployContractFromFilename(ctx context.Context, api api.FullNode, binFilename string) (address.Address, address.Address) {
	return DeployContractFromFilenameWithValue(ctx, api, binFilename, big.Zero())
}

func InvokeSolidity(ctx context.Context, api api.FullNode, sender address.Address, target address.Address, selector []byte, inputData []byte) (*api.MsgLookup, error) {
	return InvokeSolidityWithValue(ctx, api, sender, target, selector, inputData, big.Zero())
}

func InvokeSolidityWithValue(ctx context.Context, api api.FullNode, sender address.Address, target address.Address, selector []byte, inputData []byte, value big.Int) (*api.MsgLookup, error) {
	params := append(selector, inputData...)
	var buffer bytes.Buffer
	err := cbg.WriteByteArray(&buffer, params)
	if err != nil {
		return nil, err
	}
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
	if err != nil {
		return nil, err
	}

	wait, err := api.StateWaitMsg(ctx, smsg.Cid(), 3, 0, false)
	if err != nil {
		return nil, err
	}
	if !wait.Receipt.ExitCode.IsSuccess() {
		result, err := api.StateReplay(ctx, types.EmptyTSK, wait.Message)
		assert.Always(err == nil, "Replay message", map[string]interface{}{"error": err})
		fmt.Println(result)
	}
	return wait, nil
}

func NewAccount() (*key.Key, ethtypes.EthAddress, address.Address) {
	key, err := key.GenerateKey(types.KTSecp256k1)
	assert.Always(err == nil, "Generate key", map[string]interface{}{"error": err})
	ethAddr, err := ethtypes.EthAddressFromPubKey(key.PublicKey)
	assert.Always(err == nil, "Generate eth address", map[string]interface{}{"error": err})

	ea, err := ethtypes.CastEthAddress(ethAddr)
	assert.Always(err == nil, "Cast eth address", map[string]interface{}{"error": err})

	addr, err := ea.ToFilecoinAddress()
	assert.Always(err == nil, "Convert eth to filecoin address", map[string]interface{}{"error": err})

	return key, *(*ethtypes.EthAddress)(ethAddr), addr
}

func InputDataFromFrom(ctx context.Context, api api.FullNode, from address.Address) []byte {
	fromId, err := api.StateLookupID(ctx, from, types.EmptyTSK)
	assert.Always(err == nil, "Lookup ID", map[string]interface{}{"error": err})
	senderEthAddr, err := ethtypes.EthAddressFromFilecoinAddress(fromId)
	assert.Always(err == nil, "Convert filecoin to eth address", map[string]interface{}{"error": err})
	inputData := make([]byte, 32)
	copy(inputData[32-len(senderEthAddr):], senderEthAddr[:])
	return inputData
}

func InvokeContractByFuncName(ctx context.Context, api api.FullNode, fromAddr address.Address, idAddr address.Address, funcSignature string, inputData []byte) ([]byte, *api.MsgLookup, error) {
	entryPoint := CalcFuncSignature(funcSignature)
	wait, err := InvokeSolidity(ctx, api, fromAddr, idAddr, entryPoint, inputData)
	if err != nil {
		return nil, wait, err
	}
	if !wait.Receipt.ExitCode.IsSuccess() {
		result, err := api.StateReplay(ctx, types.EmptyTSK, wait.Message)
		assert.Always(err == nil, "Replay message", map[string]interface{}{"error": err})
		return nil, wait, errors.New(result.Error)
	}
	result, err := cbg.ReadByteArray(bytes.NewBuffer(wait.Receipt.Return), uint64(len(wait.Receipt.Return)))
	if err != nil {
		return nil, wait, err
	}
	return result, wait, nil
}

func CalcFuncSignature(funcName string) []byte {
	hasher := sha3.NewLegacyKeccak256()
	hasher.Write([]byte(funcName))
	hash := hasher.Sum(nil)
	return hash[:4]
}
