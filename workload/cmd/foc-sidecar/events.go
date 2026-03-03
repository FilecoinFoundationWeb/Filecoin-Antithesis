package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/big"

	"workload/internal/foc"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types/ethtypes"
)

// Event topic hashes for FWSS and related contracts.
var (
	// DataSetCreated(uint256 dataSetId, uint256 pdpRailId, uint256 providerId,
	//   uint256 clientDataSetId, uint256 filPayRailId, address payer,
	//   address serviceProvider, address payee, string[] metadataKeys, string[] metadataValues)
	TopicDataSetCreated = ethtypes.EthHash(
		([32]byte)(foc.Keccak256([]byte(
			"DataSetCreated(uint256,uint256,uint256,uint256,uint256,address,address,address,string[],string[])"))))

	// RailCreated(uint256 railId, address token, address from, address to, address operator, uint256 paymentRate, address arbiter)
	TopicRailCreated = ethtypes.EthHash(
		([32]byte)(foc.Keccak256([]byte(
			"RailCreated(uint256,address,address,address,address,uint256,address)"))))

	// FaultRecord(uint256 dataSetId, uint256 epoch, uint256 faults)
	TopicFaultRecord = ethtypes.EthHash(
		([32]byte)(foc.Keccak256([]byte(
			"FaultRecord(uint256,uint256,uint256)"))))
)

// DataSetCreatedEvent is the parsed form of a DataSetCreated log.
type DataSetCreatedEvent struct {
	DataSetID       *big.Int
	PDPRailID       *big.Int
	ProviderID      *big.Int
	ClientDataSetID *big.Int
	FilPayRailID    *big.Int
	Payer           []byte // 20 bytes
	ServiceProvider []byte // 20 bytes
	Payee           []byte // 20 bytes
}

// RailCreatedEvent is the parsed form of a RailCreated log.
type RailCreatedEvent struct {
	RailID *big.Int
	Token  []byte // 20 bytes
	From   []byte // 20 bytes
	To     []byte // 20 bytes
}

// fetchAndParseLogs retrieves logs for a given address and topic over a block range,
// returning raw EthLog entries.
func fetchAndParseLogs(ctx context.Context, node api.FullNode, contractAddr []byte, topic ethtypes.EthHash, fromBlock, toBlock uint64) ([]ethtypes.EthLog, error) {
	addr, err := ethtypes.CastEthAddress(contractAddr)
	if err != nil {
		return nil, err
	}

	fromStr := fmt.Sprintf("0x%x", fromBlock)
	toStr := fmt.Sprintf("0x%x", toBlock)

	filter := ethtypes.EthFilterSpec{
		FromBlock: &fromStr,
		ToBlock:   &toStr,
		Address:   ethtypes.EthAddressList{addr},
		Topics:    ethtypes.EthTopicSpec{ethtypes.EthHashList{topic}},
	}

	result, err := node.EthGetLogs(ctx, &filter)
	if err != nil {
		return nil, err
	}

	// EthFilterResult.Results is []interface{} — each element is a json.RawMessage
	// or a map that can be re-marshaled into EthLog.
	var logs []ethtypes.EthLog
	for _, r := range result.Results {
		raw, err := json.Marshal(r)
		if err != nil {
			continue
		}
		var ethLog ethtypes.EthLog
		if err := json.Unmarshal(raw, &ethLog); err != nil {
			continue
		}
		logs = append(logs, ethLog)
	}

	return logs, nil
}

// parseDataSetCreatedLogs extracts DataSetCreatedEvent from raw logs.
// The event data layout: dataSetId(32) | pdpRailId(32) | providerID(32) |
//
//	clientDataSetId(32) | filPayRailId(32) | payer(32) | serviceProvider(32) | payee(32) | ...
func parseDataSetCreatedLogs(logs []ethtypes.EthLog) []DataSetCreatedEvent {
	var events []DataSetCreatedEvent
	for _, l := range logs {
		data := []byte(l.Data)
		if len(data) < 256 { // minimum 8 * 32 bytes
			log.Printf("[sidecar-events] DataSetCreated log too short: %d bytes", len(data))
			continue
		}
		ev := DataSetCreatedEvent{
			DataSetID:       new(big.Int).SetBytes(data[0:32]),
			PDPRailID:       new(big.Int).SetBytes(data[32:64]),
			ProviderID:      new(big.Int).SetBytes(data[64:96]),
			ClientDataSetID: new(big.Int).SetBytes(data[96:128]),
			FilPayRailID:    new(big.Int).SetBytes(data[128:160]),
			Payer:           data[172:192],  // address at offset 160, right-aligned in 32 bytes
			ServiceProvider: data[204:224],  // offset 192
			Payee:           data[236:256],  // offset 224
		}
		events = append(events, ev)
	}
	return events
}

// parseRailCreatedLogs extracts RailCreatedEvent from raw logs.
// Data layout: railId(32) | token(32) | from(32) | to(32) | ...
func parseRailCreatedLogs(logs []ethtypes.EthLog) []RailCreatedEvent {
	var events []RailCreatedEvent
	for _, l := range logs {
		data := []byte(l.Data)
		if len(data) < 128 {
			continue
		}
		ev := RailCreatedEvent{
			RailID: new(big.Int).SetBytes(data[0:32]),
			Token:  data[44:64],
			From:   data[76:96],
			To:     data[108:128],
		}
		events = append(events, ev)
	}
	return events
}
