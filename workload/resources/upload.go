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
	assert.Always(genesisWallet != address.Undef, "failed to get genesis wallet: %v", map[string]interface{}{"error": err})

	delegatedWallet, err := CreateWallet(ctx, api, types.KTDelegated)
	assert.Always(err == nil, "Delegated wallet creation must succeed %v", map[string]interface{}{"error": err})

	ethAddr, err := ethtypes.EthAddressFromFilecoinAddress(delegatedWallet)
	assert.Always(err == nil, "failed to create Ethereum address: %v", map[string]interface{}{"error": err})

	err = SendFunds(ctx, api, genesisWallet, delegatedWallet, fundingAmount)
	assert.Always(err == nil, "failed to fund delegated wallet: %v", map[string]interface{}{"error": err})

	contractHex, err := ioutil.ReadFile(contractPath)
	assert.Always(err == nil, "failed to read contract file: %v", map[string]interface{}{"error": err})

	contract, err := hex.DecodeString(string(contractHex))
	assert.Always(err == nil, "failed to decode contract bytecode: %v", map[string]interface{}{"error": err})

	// Serialize the contract initialization parameters
	initcode := abi.CborBytes(contract)
	params, err := actors.SerializeParams(&initcode)
	assert.Always(err == nil, "failed to serialize Create params: %v", map[string]interface{}{"error": err})

	msg := &types.Message{
		To:     builtin.EthereumAddressManagerActorAddr,
		From:   delegatedWallet,
		Value:  abi.NewTokenAmount(0), // No FIL sent
		Method: builtin.MethodsEAM.CreateExternal,
		Params: params,
	}

	smsg, err := api.MpoolPushMessage(ctx, msg, nil)
	assert.Always(err == nil, "failed to push message: %v", map[string]interface{}{"error": err})

	wait, err := api.StateWaitMsg(ctx, smsg.Cid(), 0, abi.ChainEpoch(-1), true)
	assert.Always(err == nil, "error waiting for message execution: %v", map[string]interface{}{"error": err})
	assert.Always(wait.Receipt.ExitCode == 0, "smart contract deployment failed: %v", map[string]interface{}{"exitCode": wait.Receipt.ExitCode})

	var result eam.CreateReturn
	err = result.UnmarshalCBOR(bytes.NewReader(wait.Receipt.Return))
	assert.Always(err == nil, "failed to unmarshal Create return: %v", map[string]interface{}{"error": err})

	deployedEthAddr, err := ethtypes.CastEthAddress(result.EthAddress[:])
	assert.Always(err == nil, "failed to cast Ethereum address: %v", map[string]interface{}{"error": err})

	txHash, err := api.EthGetTransactionHashByCid(ctx, smsg.Cid())
	assert.Sometimes(err == nil, "failed to get Ethereum transaction hash: %v", map[string]interface{}{"error": err})

	if err == nil {
		receipt, err := api.EthGetTransactionReceipt(ctx, *txHash)
		assert.Sometimes(err == nil, "Retrieving transaction receipt should succeed sometimes", map[string]interface{}{"error": err})
		if err == nil {
			log.Printf("Transaction Receipt: %+v\n", receipt)
		}
	}
	fmt.Printf("Smart contract deployed at Ethereum address: 0x%s\n", deployedEthAddr.String())
	fmt.Printf("Using deployer Ethereum address: 0x%s\n", ethAddr.String())

	return &deployedEthAddr, nil

	
}
