package resources

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
)

// RPCResponse wraps the response data and status
type RPCResponse struct {
	StatusCode int
	Body       []byte
}

var lotusNodes = []struct {
	Name   string
	RPCURL string
}{
	{
		Name:   "Lotus1",
		RPCURL: "http://10.20.20.24:1234/rpc/v1",
	},
	{
		Name:   "Lotus2",
		RPCURL: "http://10.20.20.26:1235/rpc/v1",
	},
}

// DoRawRequest sends RPC requests to all Lotus nodes
// version: API version to use
// body: JSON-RPC request body
// returns: map of node name to RPCResponse
func DoRawRequest(version int, body string) map[string]RPCResponse {
	responses := make(map[string]RPCResponse)

	for _, node := range lotusNodes {
		// Prepare and send request
		request, err := http.NewRequest("POST", node.RPCURL, bytes.NewReader([]byte(body)))
		if err != nil {
			log.Printf("Error creating request for node %s: %v", node.Name, err)
			continue
		}

		// Set headers
		request.Header.Set("Content-Type", "application/json")

		// Send request
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			log.Printf("Error sending request to node %s: %v", node.Name, err)
			continue
		}

		// Read response
		respBody, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			log.Printf("Error reading response from node %s: %v", node.Name, err)
			continue
		}

		// Store response
		responses[node.Name] = RPCResponse{
			StatusCode: response.StatusCode,
			Body:       respBody,
		}
	}

	return responses
}

// CreateJSONRPCRequest creates a properly formatted JSON-RPC request body
func CreateJSONRPCRequest(method string, params interface{}) (string, error) {
	req := struct {
		JsonRPC string      `json:"jsonrpc"`
		Method  string      `json:"method"`
		Params  interface{} `json:"params"`
		ID      int         `json:"id"`
	}{
		JsonRPC: "2.0",
		Method:  method,
		Params:  params,
		ID:      1,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("error marshaling request: %w", err)
	}

	return string(data), nil
}
