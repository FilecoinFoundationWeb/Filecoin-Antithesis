package resources

import (
	"bytes"
	"encoding/json"
	"fmt"
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

func (rpc *RPCMethod) Stop() {
	for i := 0; i < rpc.concurrency; i++ {
		rpc.stopCh <- struct{}{}
	}
}

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
