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
		RPCURL: "http://10.20.20.24:1234/rpc/v2",
	},
	{
		Name:   "Lotus2",
		RPCURL: "http://10.20.20.26:1235/rpc/v2",
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

// GetTipSetBySelector fetches tipset using specified selector from all nodes
func GetTipSetBySelector(selector string) (map[string]RPCResponse, error) {
	params := []interface{}{map[string]string{"tag": selector}}
	requestBody, err := CreateJSONRPCRequest("Filecoin.ChainGetTipSet", params)
	if err != nil {
		return nil, err
	}

	return DoRawRequest(2, requestBody), nil
}

// GetLatestTipSet fetches the latest tipset from all nodes
func GetLatestTipSet() (map[string]RPCResponse, error) {
	return GetTipSetBySelector("latest")
}

// GetSafeTipSet fetches the safe tipset from all nodes
func GetSafeTipSet() (map[string]RPCResponse, error) {
	return GetTipSetBySelector("safe")
}

// GetFinalizedTipSet fetches the finalized tipset from all nodes
func GetFinalizedTipSet() (map[string]RPCResponse, error) {
	return GetTipSetBySelector("finalized")
}

// CompareTipSetResponses compares CIDs from multiple nodes' responses
func CompareTipSetResponses(responses map[string]RPCResponse) (bool, map[string][]string, map[string]int, error) {
	type TipSetResponse struct {
		Result struct {
			Cids []struct {
				Slash string `json:"/"`
			}
			Height int
		}
	}

	cidsByNode := make(map[string][]string)
	heightsByNode := make(map[string]int)

	for nodeName, resp := range responses {
		var tipSetResp TipSetResponse
		if err := json.Unmarshal(resp.Body, &tipSetResp); err != nil {
			return false, nil, nil, err
		}

		var cids []string
		for _, cid := range tipSetResp.Result.Cids {
			cids = append(cids, cid.Slash)
		}

		cidsByNode[nodeName] = cids
		heightsByNode[nodeName] = tipSetResp.Result.Height
	}

	// Check if all nodes have the same CIDs
	if len(cidsByNode) <= 1 {
		return true, cidsByNode, heightsByNode, nil
	}

	var referenceNode string
	var referenceCids []string

	for node, cids := range cidsByNode {
		if referenceNode == "" {
			referenceNode = node
			referenceCids = cids
			continue
		}

		if len(cids) != len(referenceCids) {
			return false, cidsByNode, heightsByNode, nil
		}

		for i, cid := range cids {
			if cid != referenceCids[i] {
				return false, cidsByNode, heightsByNode, nil
			}
		}
	}

	return true, cidsByNode, heightsByNode, nil
}

// GetTipSetByHeight fetches tipset at the specified height from all nodes
func GetTipSetByHeight(height int64) (map[string]RPCResponse, error) {
	params := []interface{}{height}
	requestBody, err := CreateJSONRPCRequest("Filecoin.ChainGetTipSetByHeight", params)
	if err != nil {
		return nil, err
	}

	return DoRawRequest(2, requestBody), nil
}

// GetTipSetBySelectorAndHeight fetches tipset using both selector and height parameters
func GetTipSetBySelectorAndHeight(selector string, height int64) (map[string]RPCResponse, error) {
	params := []interface{}{
		map[string]interface{}{
			"tag":    selector,
			"height": height,
		},
	}
	requestBody, err := CreateJSONRPCRequest("Filecoin.ChainGetTipSet", params)
	if err != nil {
		return nil, err
	}

	return DoRawRequest(2, requestBody), nil
}
