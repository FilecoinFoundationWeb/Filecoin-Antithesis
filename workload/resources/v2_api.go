package resources

import (
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"time"
)

// V2APIMethod represents a Filecoin V2 API method configuration
type V2APIMethod struct {
	Name        string
	Tag         string
	Params      interface{}
	ExpectError bool
}

// V2APIMethods returns all supported V2 API methods with their configurations
func V2APIMethods() []V2APIMethod {
	return []V2APIMethod{
		// Standard eth_chainId call
		{
			Name:   "eth_chainId",
			Tag:    "standard",
			Params: []interface{}{},
		},
		// eth_getBlockByNumber with different tags
		{
			Name:   "eth_getBlockByNumber",
			Tag:    "latest",
			Params: []interface{}{"latest", false},
		},
		{
			Name:   "eth_getBlockByNumber",
			Tag:    "finalized",
			Params: []interface{}{"finalized", false},
		},
		{
			Name:   "eth_getBlockByNumber",
			Tag:    "safe",
			Params: []interface{}{"safe", false},
		},
		{
			Name:   "eth_getBlockByNumber",
			Tag:    "pending",
			Params: []interface{}{"pending", false},
		},
		// Filecoin.ChainGetTipSet with different tags
		{
			Name:   "Filecoin.ChainGetTipSet",
			Tag:    "latest",
			Params: []interface{}{map[string]string{"tag": "latest"}},
		},
		{
			Name:   "Filecoin.ChainGetTipSet",
			Tag:    "finalized",
			Params: []interface{}{map[string]string{"tag": "finalized"}},
		},
		{
			Name:   "Filecoin.ChainGetTipSet",
			Tag:    "safe",
			Params: []interface{}{map[string]string{"tag": "safe"}},
		},
		// Edge cases
		{
			Name:        "eth_getBlockByNumber",
			Tag:         "invalid_tag",
			Params:      []interface{}{"invalid", false},
			ExpectError: true,
		},
		{
			Name:        "Filecoin.ChainGetTipSet",
			Tag:         "invalid_tag",
			Params:      []interface{}{map[string]string{"tag": "invalid"}},
			ExpectError: true,
		},
	}
}

// RunV2APITests executes tests for all V2 API methods
func RunV2APITests(endpoint string, duration time.Duration) {
	methods := V2APIMethods()
	var methodConfigs []MethodConfig

	for _, method := range methods {
		params, err := json.Marshal(method.Params)
		if err != nil {
			log.Printf("[ERROR] Failed to marshal params for %s (%s): %v", method.Name, method.Tag, err)
			continue
		}

		methodConfigs = append(methodConfigs, MethodConfig{
			Method:        method.Name,
			Concurrency:   1,
			QPS:           0,
			Params:        string(params),
			PrintResponse: true,
		})
	}

	RunBenchmark(endpoint, duration, methodConfigs)
}

// RunV2APILoadTest executes load testing for V2 API methods
func RunV2APILoadTest(endpoint string, duration time.Duration, concurrency int, qps int) {
	methods := V2APIMethods()
	var methodConfigs []MethodConfig

	for _, method := range methods {
		// Skip error cases for load testing
		if method.ExpectError {
			continue
		}

		params, err := json.Marshal(method.Params)
		if err != nil {
			log.Printf("[ERROR] Failed to marshal params for %s (%s): %v", method.Name, method.Tag, err)
			continue
		}

		methodConfigs = append(methodConfigs, MethodConfig{
			Method:        method.Name,
			Concurrency:   concurrency,
			QPS:           qps,
			Params:        string(params),
			PrintResponse: false,
		})
	}

	RunBenchmark(endpoint, duration, methodConfigs)
}

// RunV2APIEdgeCases tests edge cases and error conditions
func RunV2APIEdgeCases(endpoint string) {
	// Large block number test
	bigBlockNum := new(big.Int)
	bigBlockNum.SetString("1000000000000000000000000000000000000", 10)

	edgeCases := []V2APIMethod{
		{
			Name:        "eth_getBlockByNumber",
			Tag:         "large_number",
			Params:      []interface{}{fmt.Sprintf("0x%x", bigBlockNum), false},
			ExpectError: true,
		},
		{
			Name:        "eth_getBlockByNumber",
			Tag:         "negative",
			Params:      []interface{}{"-1", false},
			ExpectError: true,
		},
		{
			Name:        "eth_getBlockByNumber",
			Tag:         "malformed_hex",
			Params:      []interface{}{"0xINVALID", false},
			ExpectError: true,
		},
		{
			Name:        "Filecoin.ChainGetTipSet",
			Tag:         "empty_params",
			Params:      []interface{}{},
			ExpectError: true,
		},
		{
			Name:        "Filecoin.ChainGetTipSet",
			Tag:         "null_params",
			Params:      []interface{}{nil},
			ExpectError: true,
		},
	}

	var methodConfigs []MethodConfig
	for _, edgeCase := range edgeCases {
		params, err := json.Marshal(edgeCase.Params)
		if err != nil {
			log.Printf("[ERROR] Failed to marshal params for edge case %s (%s): %v", edgeCase.Name, edgeCase.Tag, err)
			continue
		}

		methodConfigs = append(methodConfigs, MethodConfig{
			Method:        edgeCase.Name,
			Concurrency:   1,
			QPS:           0,
			Params:        string(params),
			PrintResponse: true,
		})
	}

	RunBenchmark(endpoint, 5*time.Second, methodConfigs)
}

// RunV2APIBatchTest tests batch requests
func RunV2APIBatchTest(endpoint string) {
	methods := V2APIMethods()
	var batchRequests []map[string]interface{}

	for i, method := range methods {
		if !method.ExpectError {
			batchRequests = append(batchRequests, map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      i,
				"method":  method.Name,
				"params":  method.Params,
			})
		}
	}

	batchJSON, err := json.Marshal(batchRequests)
	if err != nil {
		log.Printf("[ERROR] Failed to marshal batch requests: %v", err)
		return
	}

	methodConfigs := []MethodConfig{
		{
			Method:        "batch",
			Concurrency:   1,
			QPS:           0,
			Params:        string(batchJSON),
			PrintResponse: true,
		},
	}

	RunBenchmark(endpoint, 5*time.Second, methodConfigs)
}
