package resources

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"log"

	"github.com/antithesishq/antithesis-sdk-go/assert"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/builtin"
	"github.com/filecoin-project/go-state-types/builtin/v10/eam"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/types/ethtypes"
)

// DeploySmartContract deploys a smart contract using a delegated wallet.
func DeploySmartContract(ctx context.Context, api api.FullNode, contractPath string, fundingAmount abi.TokenAmount) (*ethtypes.EthAddress, error) {
	genesisWallet, err := GetGenesisWallet(ctx, api)
	assert.Sometimes(genesisWallet != address.Undef, "Get the genesis wallet", map[string]interface{}{"error": err})

	delegatedWallet, err := CreateWallet(ctx, api, types.KTDelegated)
	assert.Always(err == nil, "Create a delegated type wallet", map[string]interface{}{"error": err})

	ethAddr, err := ethtypes.EthAddressFromFilecoinAddress(delegatedWallet)
	assert.Always(err == nil, "Create an Ethereum address from a Filecoin address", map[string]interface{}{"error": err})

	err = SendFunds(ctx, api, genesisWallet, delegatedWallet, fundingAmount)
	assert.Sometimes(err == nil, "Fund a delegated wallet", map[string]interface{}{"genesisWallet": genesisWallet, "delegatedWallet": delegatedWallet, "amount": fundingAmount, "error": err})

	contractHex, err := ioutil.ReadFile(contractPath)
	assert.Always(err == nil, "Read the smart contract file", map[string]interface{}{"filePath": contractPath, "error": err})

	contract, err := hex.DecodeString(string(contractHex))
	assert.Always(err == nil, "Decode smart contract into a byte representation", map[string]interface{}{"hex": string(contractHex), "error": err})

	// Serialize the contract initialization parameters
	initcode := abi.CborBytes(contract)
	params, err := actors.SerializeParams(&initcode)
	assert.Always(err == nil, "Serialize initial smart contract bytecodes to Filecoin compatible format", map[string]interface{}{"error": err})

	msg := &types.Message{
		To:     builtin.EthereumAddressManagerActorAddr,
		From:   delegatedWallet,
		Value:  abi.NewTokenAmount(0), // No FIL sent
		Method: builtin.MethodsEAM.CreateExternal,
		Params: params,
	}

	smsg, err := api.MpoolPushMessage(ctx, msg, nil)
	assert.Sometimes(err == nil, "Push a smart contract message", map[string]interface{}{"error": err})

	if smsg == nil {
		log.Fatalf("Failed to push message to mempool: smsg is nil, error: %v", err)
	}

	wait, err := api.StateWaitMsg(ctx, smsg.Cid(), 5, 100, false)
	assert.Sometimes(err == nil, "Failed while waiting for the message to land on chain", map[string]interface{}{"Cid": smsg.Cid(), "error": err})

	// Replay check for failed execution
	if !wait.Receipt.ExitCode.IsSuccess() {
		result, replayErr := api.StateReplay(ctx, types.EmptyTSK, smsg.Cid())
		assert.Sometimes(replayErr == nil, "StateReplay failed", map[string]interface{}{"messageCid": smsg.Cid(), "error": replayErr})
		assert.Always(result != nil, "StateReplay returned nil result", map[string]interface{}{"messageCid": smsg.Cid()})
		return nil, fmt.Errorf("smart contract deployment failed: %v", result.Error)
	}

	var result eam.CreateReturn
	err = result.UnmarshalCBOR(bytes.NewReader(wait.Receipt.Return))
	assert.Always(err == nil, "Unmarshal CBOR", map[string]interface{}{"error": err})

	deployedEthAddr, err := ethtypes.CastEthAddress(result.EthAddress[:])
	assert.Always(err == nil, "Interpret bytes as an EthAddress and perform basic checks by casting", map[string]interface{}{"error": err})

	txHash, err := api.EthGetTransactionHashByCid(ctx, smsg.Cid())
	assert.Sometimes(err == nil, "Get Ethereum transaction hash from the Chain ID", map[string]interface{}{"error": err})

	if err == nil {
		receipt, err := api.EthGetTransactionReceipt(ctx, *txHash)
		assert.Sometimes(err == nil, "Retrieve transaction receipt from Eth transaction hash", map[string]interface{}{"error": err})
		if err == nil {
			log.Printf("Transaction Receipt: %+v\n", receipt)
		}
	}

	fmt.Printf("Smart contract deployed at Ethereum address: 0x%s\n", deployedEthAddr.String())
	fmt.Printf("Using deployer Ethereum address: 0x%s\n", ethAddr.String())

	return &deployedEthAddr, nil
}
