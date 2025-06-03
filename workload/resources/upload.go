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
	"github.com/filecoin-project/go-state-types/crypto"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build/buildconstants"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/types/ethtypes"
	"github.com/filecoin-project/lotus/chain/wallet/key"
	"github.com/filecoin-project/lotus/lib/sigs"
	_ "github.com/filecoin-project/lotus/lib/sigs/delegated"
	_ "github.com/filecoin-project/lotus/lib/sigs/secp"
	"github.com/multiformats/go-varint"
	cbg "github.com/whyrusleeping/cbor-gen"
	"golang.org/x/crypto/sha3"
)

func DeployContractWithValue(ctx context.Context, api api.FullNode, sender address.Address, bytecode []byte, value big.Int) eam.CreateReturn {
	var result eam.CreateReturn

	method := builtin.MethodsEAM.CreateExternal
	initcode := abi.CborBytes(bytecode)

	params, Actorerr := actors.SerializeParams(&initcode)
	if Actorerr != nil {
		log.Printf("Failed to serialize contract initialization parameters: %v", Actorerr)
		return result
	}

	msg := &types.Message{
		To:     builtin.EthereumAddressManagerActorAddr,
		From:   sender,
		Value:  value,
		Method: method,
		Params: params,
	}

	smsg, err := api.MpoolPushMessage(ctx, msg, nil)
	if err != nil {
		log.Printf("Failed to push create contract message: %v", err)
		return result
	}
	time.Sleep(5 * time.Second)

	wait, err := api.StateWaitMsg(ctx, smsg.Cid(), 5, 100, false)
	if err != nil {
		log.Printf("Error waiting for contract creation message: %v", err)
		return result
	}

	if !wait.Receipt.ExitCode.IsSuccess() {
		log.Printf("Contract installation failed with exit code: %v", wait.Receipt.ExitCode)
		return result
	}

	r := bytes.NewReader(wait.Receipt.Return)
	err = result.UnmarshalCBOR(r)
	if err != nil {
		log.Printf("Failed to unmarshal CBOR result: %v", err)
		return result
	}

	return result
}

func DeployContract(ctx context.Context, api api.FullNode, sender address.Address, bytecode []byte) eam.CreateReturn {
	return DeployContractWithValue(ctx, api, sender, bytecode, big.Zero())
}

func DeployContractFromFilenameWithValue(ctx context.Context, api api.FullNode, binFilename string, value big.Int) (address.Address, address.Address) {
	// Check if file exists
	if _, err := os.Stat(binFilename); os.IsNotExist(err) {
		log.Printf("[ERROR] Contract file not found: %s", binFilename)
		return address.Address{}, address.Address{}
	}

	contractHex, err := os.ReadFile(binFilename)
	if err != nil {
		log.Printf("[ERROR] Failed to read contract file %s: %v", binFilename, err)
		return address.Address{}, address.Address{}
	}

	contractHex = bytes.TrimRight(contractHex, "\n")
	contract, err := hex.DecodeString(string(contractHex))
	if err != nil {
		log.Printf("[ERROR] Failed to decode hex string: %v", err)
		return address.Address{}, address.Address{}
	}

	fromAddr, err := api.WalletDefaultAddress(ctx)
	if err != nil || fromAddr.Empty() {
		log.Printf("[ERROR] No default wallet address found: %v", err)

		// Try to get any wallet address
		addresses, err := api.WalletList(ctx)
		if err != nil || len(addresses) == 0 {
			log.Printf("[ERROR] No wallet addresses available: %v", err)
			return address.Address{}, address.Address{}
		}

		// Use the first address
		fromAddr = addresses[0]
		log.Printf("[INFO] Using wallet address: %s", fromAddr)
	}

	// Verify address has funds
	balance, err := api.WalletBalance(ctx, fromAddr)
	if err != nil {
		log.Printf("[WARN] Failed to check wallet balance: %v", err)
	} else if balance.IsZero() {
		log.Printf("[WARN] Wallet has zero balance, deployment may fail")
	}

	result := DeployContractWithValue(ctx, api, fromAddr, contract, value)

	if result.ActorID == 0 {
		log.Printf("[ERROR] Contract deployment failed, got ActorID 0")
		return fromAddr, address.Address{}
	}

	idAddr, err := address.NewIDAddress(result.ActorID)
	if err != nil {
		log.Printf("[ERROR] Failed to create ID address from ActorID %d: %v", result.ActorID, err)
		return fromAddr, address.Address{}
	}

	log.Printf("[INFO] Contract deployed from %s to %s (Actor ID: %d)", fromAddr, idAddr, result.ActorID)
	return fromAddr, idAddr
}

func DeployContractFromFilename(ctx context.Context, api api.FullNode, binFilename string) (address.Address, address.Address) {
	fromAddr, contractAddr := DeployContractFromFilenameWithValue(ctx, api, binFilename, big.Zero())

	return fromAddr, contractAddr
}

func InvokeSolidity(ctx context.Context, api api.FullNode, sender address.Address, target address.Address, selector []byte, inputData []byte) (*api.MsgLookup, error) {
	return InvokeSolidityWithValue(ctx, api, sender, target, selector, inputData, big.Zero())
}

func InvokeContractByFuncName(ctx context.Context, api api.FullNode, fromAddr address.Address, idAddr address.Address, funcSignature string, inputData []byte) ([]byte, *api.MsgLookup, error) {
	entryPoint := CalcFuncSignature(funcSignature)

	wait, err := InvokeSolidity(ctx, api, fromAddr, idAddr, entryPoint, inputData)
	if err != nil {
		log.Printf("Failed to invoke Solidity function: %v", err)
		return nil, wait, fmt.Errorf("failed to invoke Solidity function: %w", err)
	}

	if !wait.Receipt.ExitCode.IsSuccess() {
		replayResult, replayErr := api.StateReplay(ctx, types.EmptyTSK, wait.Message)
		if replayErr != nil {
			log.Printf("Failed to replay failed message: %v", replayErr)
			return nil, wait, fmt.Errorf("failed to replay failed message: %w", replayErr)
		}
		return nil, wait, fmt.Errorf("invoke failed with error: %v", replayResult.Error)
	}

	result, err := cbg.ReadByteArray(bytes.NewBuffer(wait.Receipt.Return), uint64(len(wait.Receipt.Return)))
	if err != nil {
		log.Printf("Failed to read return data from contract execution: %v", err)
	}
	return result, wait, err
}

func InvokeSolidityWithValue(ctx context.Context, api api.FullNode, sender address.Address, target address.Address, selector []byte, inputData []byte, value big.Int) (*api.MsgLookup, error) {
	params := append(selector, inputData...)
	var buffer bytes.Buffer
	err := cbg.WriteByteArray(&buffer, params)
	if err != nil {
		log.Printf("Failed to write byte array to buffer: %v", err)
		return nil, fmt.Errorf("failed to write byte array to buffer: %w", err)
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
		log.Printf("Failed to push message to invoke contract: %v", err)
		return nil, err
	}
	time.Sleep(5 * time.Second)

	wait, err := api.StateWaitMsg(ctx, smsg.Cid(), 5, 100, false)
	if err != nil {
		log.Printf("Error waiting for invoke message to execute: %v", err)
		return nil, err
	}

	if !wait.Receipt.ExitCode.IsSuccess() {
		log.Print("Contract invocation failed")
		replayResult, err := api.StateReplay(ctx, types.EmptyTSK, wait.Message)
		log.Printf("StateReplay Error (err): %s", err)
		if replayResult != nil {
			log.Printf("StateReplay Error (replayResult.Error): %s", replayResult.Error)
			return nil, fmt.Errorf("invoke failed with error: %v", replayResult.Error)
		}
		return nil, fmt.Errorf("invoke failed and failed to replay: %v", err)
	}
	return wait, nil
}

func CalcFuncSignature(funcName string) []byte {
	hasher := sha3.NewLegacyKeccak256()
	hasher.Write([]byte(funcName))
	return hasher.Sum(nil)[:4]
}

func InputDataFromFrom(ctx context.Context, api api.FullNode, from address.Address) []byte {
	if from.Empty() {
		log.Printf("[ERROR] Cannot process empty 'from' address")
		return make([]byte, 32) // Return empty data instead of panicking
	}

	fromID, err := api.StateLookupID(ctx, from, types.EmptyTSK)
	if err != nil {
		log.Printf("[ERROR] Failed to lookup ID for address %s: %v", from, err)
		return make([]byte, 32) // Return empty data instead of panicking
	}

	senderEthAddr, err := ethtypes.EthAddressFromFilecoinAddress(fromID)
	if err != nil {
		log.Printf("[ERROR] Failed to convert address %s to Ethereum format: %v", fromID, err)
		return make([]byte, 32) // Return empty data instead of panicking
	}

	inputData := make([]byte, 32)
	copy(inputData[32-len(senderEthAddr):], senderEthAddr[:])
	return inputData
}

func SignTransaction(tx *ethtypes.Eth1559TxArgs, privKey []byte) {
	preimage, err := tx.ToRlpUnsignedMsg()
	if err != nil {
		log.Printf("Failed to convert transaction to RLP: %v", err)
		return
	}
	signature, err := sigs.Sign(crypto.SigTypeDelegated, privKey, preimage)
	if err != nil {
		log.Printf("Failed to sign transaction: %v", err)
		return
	}
	err = tx.InitialiseSignature(*signature)
	if err != nil {
		log.Printf("Failed to initialise signature: %v", err)
		return
	}
}

func SubmitTransaction(ctx context.Context, api api.FullNode, tx ethtypes.EthTransaction) ethtypes.EthHash {
	signed, err := tx.ToRlpSignedMsg()
	if err != nil {
		log.Printf("Failed to convert transaction to RLP: %v", err)
		return ethtypes.EthHash{}
	}
	txHash, err := api.EthSendRawTransaction(ctx, signed)
	if err != nil {
		log.Printf("Failed to send transaction: %v", err)
		return ethtypes.EthHash{}
	}
	return txHash
}

func AssertAddressBalanceConsistent(ctx context.Context, api api.FullNode, addr address.Address) {
	payload := addr.Payload()
	_, _, err := varint.FromUvarint(payload)
	if err != nil {
		log.Printf("Failed to get namespace from address: %v", err)
		return
	}
	fbal, err := api.WalletBalance(ctx, addr)
	if err != nil {
		log.Printf("Failed to get balance for address: %v", err)
		return
	}

	ethAddr, err := ethtypes.EthAddressFromFilecoinAddress(addr)
	if err != nil {
		log.Printf("Failed to get Ethereum address for address: %v", err)
		return
	}

	ebal, err := api.EthGetBalance(ctx, ethAddr, ethtypes.NewEthBlockNumberOrHashFromPredefined("latest"))
	if err != nil {
		log.Printf("Failed to get Ethereum balance for address: %v", err)
		return
	}
	assert.Always(fbal.Equals(types.BigInt(ebal)), "Address balance inconsistent", map[string]any{
		"filecoin_balance": fbal,
		"ethereum_balance": ebal,
	})
}

func NewAccount() (*key.Key, ethtypes.EthAddress, address.Address) {
	// Generate a secp256k1 key; this will back the Ethereum identity.
	key, err := key.GenerateKey(types.KTSecp256k1)
	if err != nil {
		log.Printf("Failed to generate key: %v", err)
		return nil, ethtypes.EthAddress{}, address.Address{}
	}

	ethAddr, err := ethtypes.EthAddressFromPubKey(key.PublicKey)
	if err != nil {
		log.Printf("Failed to generate Ethereum address: %v", err)
		return nil, ethtypes.EthAddress{}, address.Address{}
	}

	ea, err := ethtypes.CastEthAddress(ethAddr)
	if err != nil {
		log.Printf("Failed to cast Ethereum address: %v", err)
		return nil, ethtypes.EthAddress{}, address.Address{}
	}

	addr, err := ea.ToFilecoinAddress()
	if err != nil {
		log.Printf("Failed to convert Ethereum address to Filecoin address: %v", err)
		return nil, ethtypes.EthAddress{}, address.Address{}
	}
	return key, *(*ethtypes.EthAddress)(ethAddr), addr
}

func SignLegacyHomesteadTransaction(tx *ethtypes.EthLegacyHomesteadTxArgs, privKey []byte) {
	preimage, err := tx.ToRlpUnsignedMsg()
	if err != nil {
		log.Printf("Failed to convert transaction to RLP: %v", err)
		return
	}

	// sign the RLP payload
	signature, err := sigs.Sign(crypto.SigTypeDelegated, privKey, preimage)
	if err != nil {
		log.Printf("Failed to sign transaction: %v", err)
		return
	}

	signature.Data = append([]byte{ethtypes.EthLegacyHomesteadTxSignaturePrefix}, signature.Data...)
	signature.Data[len(signature.Data)-1] += 27

	err = tx.InitialiseSignature(*signature)
	if err != nil {
		log.Printf("Failed to initialise signature: %v", err)
		return
	}
}
