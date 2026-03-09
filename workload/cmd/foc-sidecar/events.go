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
	// DataSetCreated(uint256 indexed dataSetId, uint256 indexed providerId,
	//   uint256 pdpRailId, uint256 cacheMissRailId, uint256 cdnRailId,
	//   address payer, address serviceProvider, address payee,
	//   string[] metadataKeys, string[] metadataValues)
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

	// DataSetDeleted(uint256 dataSetId, uint256 deletedLeafCount)
	TopicDataSetDeleted = ethtypes.EthHash(
		([32]byte)(foc.Keccak256([]byte(
			"DataSetDeleted(uint256,uint256)"))))
)

// DataSetCreatedEvent is the parsed form of a DataSetCreated log.
type DataSetCreatedEvent struct {
	DataSetID       *big.Int // indexed — from Topics[1]
	ProviderID      *big.Int // indexed — from Topics[2]
	PDPRailID       *big.Int // from Data
	CacheMissRailID *big.Int // from Data
	CDNRailID       *big.Int // from Data
	Payer           []byte   // 20 bytes
	ServiceProvider []byte   // 20 bytes
	Payee           []byte   // 20 bytes
}

// RailCreatedEvent is the parsed form of a RailCreated log.
type RailCreatedEvent struct {
	RailID *big.Int
	Token  []byte // 20 bytes
	From   []byte // 20 bytes
	To     []byte // 20 bytes
}

// DataSetDeletedEvent is the parsed form of a DataSetDeleted log.
type DataSetDeletedEvent struct {
	DataSetID       *big.Int
	DeletedLeafCount *big.Int
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
// Indexed fields (dataSetId, providerId) are in Topics[1] and Topics[2].
// Non-indexed data layout: pdpRailId(32) | cacheMissRailId(32) | cdnRailId(32) |
//
//	payer(32) | serviceProvider(32) | payee(32) | ... (dynamic string[] arrays)
func parseDataSetCreatedLogs(logs []ethtypes.EthLog) []DataSetCreatedEvent {
	var events []DataSetCreatedEvent
	for _, l := range logs {
		if len(l.Topics) < 3 {
			log.Printf("[sidecar-events] DataSetCreated log has only %d topics, need 3", len(l.Topics))
			continue
		}
		data := []byte(l.Data)
		if len(data) < 192 { // minimum 6 * 32 bytes (3 uint256 + 3 address)
			log.Printf("[sidecar-events] DataSetCreated log too short: %d bytes", len(data))
			continue
		}
		ev := DataSetCreatedEvent{
			DataSetID:       new(big.Int).SetBytes(l.Topics[1][:]),  // indexed
			ProviderID:      new(big.Int).SetBytes(l.Topics[2][:]),  // indexed
			PDPRailID:       new(big.Int).SetBytes(data[0:32]),
			CacheMissRailID: new(big.Int).SetBytes(data[32:64]),
			CDNRailID:       new(big.Int).SetBytes(data[64:96]),
			Payer:           data[108:128], // address at offset 96, right-aligned in 32 bytes
			ServiceProvider: data[140:160], // offset 128
			Payee:           data[172:192], // offset 160
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

// parseDataSetDeletedLogs extracts DataSetDeletedEvent from raw logs.
// Data layout: dataSetId(32) | deletedLeafCount(32)
func parseDataSetDeletedLogs(logs []ethtypes.EthLog) []DataSetDeletedEvent {
	var events []DataSetDeletedEvent
	for _, l := range logs {
		data := []byte(l.Data)
		if len(data) < 64 {
			continue
		}
		ev := DataSetDeletedEvent{
			DataSetID:        new(big.Int).SetBytes(data[0:32]),
			DeletedLeafCount: new(big.Int).SetBytes(data[32:64]),
		}
		events = append(events, ev)
	}
	return events
}
