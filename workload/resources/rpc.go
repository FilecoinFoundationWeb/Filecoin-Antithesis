package resources

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

type MethodConfig struct {
	Method        string
	Concurrency   int
	QPS           int
	Params        string
	PrintResponse bool
}

type RPCMethod struct {
	uri         string
	method      string
	concurrency int
	qps         int
	params      string
	printResp   bool
	stopCh      chan struct{}
	start       time.Time
}

// buildRequest creates a JSON-RPC request with the specified method and parameters
func (rpc *RPCMethod) buildRequest() (*http.Request, error) {
	jreq, err := json.Marshal(struct {
		Jsonrpc string          `json:"jsonrpc"`
		ID      int             `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}{
		Jsonrpc: "2.0",
		Method:  rpc.method,
		Params:  json.RawMessage(rpc.params),
		ID:      0,
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", rpc.uri, bytes.NewReader(jreq))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	return req, nil
}

// Stop signals all goroutines to stop processing requests
func (rpc *RPCMethod) Stop() {
	for i := 0; i < rpc.concurrency; i++ {
		rpc.stopCh <- struct{}{}
	}
}

// RunAndLog executes RPC requests with specified concurrency and QPS limits
// It tracks and logs metrics like latency, request count, and errors
func (rpc *RPCMethod) RunAndLog() error {
	client := &http.Client{Timeout: 0}
	var wg sync.WaitGroup
	wg.Add(rpc.concurrency)

	rpc.stopCh = make(chan struct{}, rpc.concurrency)
	rpc.start = time.Now()

	var qpsTicker *time.Ticker
	if rpc.qps > 0 {
		qpsTicker = time.NewTicker(time.Second / time.Duration(rpc.qps))
	}

	// Track latency metrics
	var totalDuration time.Duration
	var requestCount int64
	var errorCount int64
	var metricsMutex sync.Mutex

	for i := 0; i < rpc.concurrency; i++ {
		go func() {
			defer wg.Done()
			for {
				select {
				case <-rpc.stopCh:
					return
				default:
				}

				if qpsTicker != nil {
					<-qpsTicker.C
				}

				req, err := rpc.buildRequest()
				if err != nil {
					metricsMutex.Lock()
					errorCount++
					metricsMutex.Unlock()
					fmt.Printf("[ERROR][%s] Failed to build request: %v\n", rpc.method, err)
					continue
				}

				startTime := time.Now()
				resp, err := client.Do(req)
				duration := time.Since(startTime)

				// Update metrics
				metricsMutex.Lock()
				totalDuration += duration
				requestCount++
				if err != nil {
					errorCount++
					fmt.Printf("[ERROR][%s] Request failed: %v | Duration: %v\n", rpc.method, err, duration)
				}
				metricsMutex.Unlock()

				if resp != nil {
					resp.Body.Close()
				}
			}
		}()
	}

	wg.Wait()

	// Print final statistics
	fmt.Printf("\n[SUMMARY][%s]\n", rpc.method)
	fmt.Printf("Total Requests: %d\n", requestCount)
	fmt.Printf("Error Count: %d\n", errorCount)
	if requestCount > 0 {
		avgLatency := totalDuration / time.Duration(requestCount)
		fmt.Printf("Average Latency: %v\n", avgLatency)
	}
	fmt.Println()

	return nil
}

// RunBenchmark executes multiple RPC methods concurrently for a specified duration
// Each method can have its own concurrency and QPS settings
func RunBenchmark(endpoint string, duration time.Duration, methods []MethodConfig) {
	var rpcMethods []*RPCMethod
	for _, mc := range methods {
		rpcMethods = append(rpcMethods, &RPCMethod{
			uri:         endpoint,
			method:      mc.Method,
			concurrency: mc.Concurrency,
			qps:         mc.QPS,
			params:      mc.Params,
			printResp:   mc.PrintResponse,
		})
	}

	var wg sync.WaitGroup
	wg.Add(len(rpcMethods))

	go func() {
		time.Sleep(duration)
		for _, m := range rpcMethods {
			m.Stop()
		}
	}()

	for _, m := range rpcMethods {
		go func(m *RPCMethod) {
			defer wg.Done()
			if err := m.RunAndLog(); err != nil {
				fmt.Printf("Error running method %s: %v\n", m.method, err)
			}
		}(m)
	}

	wg.Wait()
}

// DoRawRPCRequest performs a single JSON-RPC request to the specified endpoint
// Returns the HTTP status code and response body
func DoRawRPCRequest(endpoint string, version int, body string) (int, []byte) {
	endpoint = fmt.Sprintf("%s/rpc/v%d", endpoint, version)
	request, err := http.NewRequest("POST", endpoint, bytes.NewReader([]byte(body)))
	if err != nil {
		return 0, nil
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return 0, nil
	}
	defer func() {
		if err := response.Body.Close(); err != nil {
			log.Printf("Failed to close response body: %v", err)
		}
	}()
	respBody, err := io.ReadAll(response.Body)
	if err != nil {
		return 0, nil
	}
	return response.StatusCode, respBody
}

// CallV2API calls V2 API tests
func CallV2API(endpoint string) {
	log.Printf("[INFO] Starting V2 API tests on endpoint: %s", endpoint)

	// Run standard tests
	log.Printf("[INFO] Running standard V2 API tests...")
	RunV2APITests(endpoint, 5*time.Second)

	// Run load tests
	log.Printf("[INFO] Running V2 API load tests...")
	RunV2APILoadTest(endpoint, 10*time.Second, 5, 10)

	log.Printf("[INFO] V2 API testing completed")
}

// CheckF3Running checks if F3 is running on nodes
func CheckF3Running() error {
	nodeMap := map[string]string{
		"http://forest:23456": "Forest",
		"http://lotus-1:1234": "Lotus1",
		"http://lotus-2:1235": "Lotus2",
	}

	request := `{"jsonrpc":"2.0","method":"Filecoin.F3IsRunning","params":[],"id":1}`

	for url, nodeName := range nodeMap {
		_, resp := DoRawRPCRequest(url, 1, request)
		var response struct {
			Result bool `json:"result"`
		}
		if err := json.Unmarshal(resp, &response); err != nil {
			log.Printf("[WARN] Failed to parse response from %s: %v", url, err)
			continue
		}

		log.Printf("[INFO] F3 is running on %s: %v", url, response.Result)
		AssertSometimes(nodeName, response.Result, fmt.Sprintf("F3 status check: F3 should be running on %s - F3 service failure detected", url),
			map[string]interface{}{
				"operation":   "f3_status_check",
				"requirement": fmt.Sprintf("F3 is running on %s", url),
			})
	}
	return nil
}

// CheckPeers checks peer connections
func CheckPeers() error {
	urls := []string{
		"http://forest:3456",
		"http://lotus-1:1234",
		"http://lotus-2:1235",
	}

	request := `{"jsonrpc":"2.0","method":"Filecoin.NetPeers","params":[],"id":1}`

	for _, url := range urls {
		_, resp := DoRawRPCRequest(url, 1, request)
		var response struct {
			Result []struct {
				ID string `json:"ID"`
			} `json:"result"`
		}
		if err := json.Unmarshal(resp, &response); err != nil {
			log.Printf("[WARN] Failed to parse response from %s: %v", url, err)
			continue
		}

		peerCount := len(response.Result)
		if peerCount == 0 {
			log.Printf("[INFO] Node %s has no peers (may be intentionally disconnected for reorg simulation)", url)
		} else {
			log.Printf("[INFO] Node %s has %d peers", url, peerCount)
		}
	}

	log.Printf("[INFO] Peer information logged for all nodes")
	return nil
}
