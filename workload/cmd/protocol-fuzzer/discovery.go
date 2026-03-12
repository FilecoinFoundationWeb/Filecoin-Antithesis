package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
)

// TargetNode represents a Filecoin node reachable via libp2p.
type TargetNode struct {
	Name     string
	AddrInfo peer.AddrInfo
}

const (
	discoveryRetryInterval = 5 * time.Second
	discoveryTimeout       = 5 * time.Minute
)

// discoverNodes reads multiaddr files written by each node's startup script.
// Each node writes its listening address to {devgenDir}/{name}/{name}-ipv4addr.
// The file contains a full multiaddr like /ip4/172.x.x.x/tcp/XXXX/p2p/12D3Koo...
func discoverNodes(names []string, devgenDir string) []TargetNode {
	var targets []TargetNode
	for _, name := range names {
		addrFile := fmt.Sprintf("%s/%s/%s-ipv4addr", devgenDir, name, name)

		data, err := os.ReadFile(addrFile)
		if err != nil {
			log.Printf("[protocol-fuzzer] skipping %s: cannot read %s: %v", name, addrFile, err)
			continue
		}

		addrStr := strings.TrimSpace(string(data))
		if addrStr == "" {
			log.Printf("[protocol-fuzzer] skipping %s: empty address file %s", name, addrFile)
			continue
		}

		ma, err := multiaddr.NewMultiaddr(addrStr)
		if err != nil {
			log.Printf("[protocol-fuzzer] skipping %s: invalid multiaddr %q: %v", name, addrStr, err)
			continue
		}

		ai, err := peer.AddrInfoFromP2pAddr(ma)
		if err != nil {
			log.Printf("[protocol-fuzzer] skipping %s: cannot parse AddrInfo from %q: %v", name, addrStr, err)
			continue
		}

		targets = append(targets, TargetNode{Name: name, AddrInfo: *ai})
		log.Printf("[protocol-fuzzer] found %s: peer=%s addr=%s", name, ai.ID.String()[:16], addrStr)
	}
	return targets
}

// waitForNodes retries discovery until at least one node is found or timeout.
func waitForNodes(names []string, devgenDir string) []TargetNode {
	deadline := time.Now().Add(discoveryTimeout)

	for time.Now().Before(deadline) {
		targets := discoverNodes(names, devgenDir)
		if len(targets) > 0 {
			log.Printf("[protocol-fuzzer] found %d/%d nodes", len(targets), len(names))
			return targets
		}
		log.Printf("[protocol-fuzzer] no nodes found yet, retrying in %s...", discoveryRetryInterval)
		time.Sleep(discoveryRetryInterval)
	}

	log.Fatal("[protocol-fuzzer] FATAL: no nodes found within timeout")
	return nil
}

// loadNetworkName reads the network name written by lotus0 at startup.
func loadNetworkName(devgenDir string) string {
	path := fmt.Sprintf("%s/lotus0/network_name", devgenDir)

	deadline := time.Now().Add(discoveryTimeout)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			name := strings.TrimSpace(string(data))
			if name != "" {
				log.Printf("[protocol-fuzzer] network name: %s", name)
				return name
			}
		}
		time.Sleep(discoveryRetryInterval)
	}

	log.Fatal("[protocol-fuzzer] FATAL: cannot read network name from " + path)
	return ""
}

// chainHeadInfo holds the current chain head state from a node's RPC.
type chainHeadInfo struct {
	CIDs   []cid.Cid
	Height uint64
}

// rpcURLForTarget returns the JSON-RPC URL for the given node name.
func rpcURLForTarget(name string) string {
	port := envOrDefault("STRESS_RPC_PORT", "1234")
	if strings.HasPrefix(name, "forest") {
		port = envOrDefault("STRESS_FOREST_RPC_PORT", "3456")
	}
	return fmt.Sprintf("http://%s:%s/rpc/v1", name, port)
}

// fetchChainHead calls Filecoin.ChainHead on the target node and returns
// the tipset CIDs and height. Returns nil on any error (non-fatal).
func fetchChainHead(targetName string) *chainHeadInfo {
	type rpcRequest struct {
		Jsonrpc string `json:"jsonrpc"`
		Method  string `json:"method"`
		Params  []any  `json:"params"`
		ID      int    `json:"id"`
	}
	type rpcResponse struct {
		Result struct {
			Cids   []struct{ Root string `json:"/"` } `json:"Cids"`
			Height int64                               `json:"Height"`
		} `json:"result"`
	}

	rpcURL := rpcURLForTarget(targetName)
	reqBody, _ := json.Marshal(rpcRequest{
		Jsonrpc: "2.0",
		Method:  "Filecoin.ChainHead",
		Params:  []any{},
		ID:      1,
	})

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(rpcURL, "application/json", bytes.NewReader(reqBody))
	if err != nil {
		debugLog("[chain-head] RPC to %s failed: %v", targetName, err)
		return nil
	}
	defer resp.Body.Close()

	var rpcResp rpcResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		debugLog("[chain-head] decode failed for %s: %v", targetName, err)
		return nil
	}

	if len(rpcResp.Result.Cids) == 0 {
		debugLog("[chain-head] empty CIDs from %s", targetName)
		return nil
	}

	var cids []cid.Cid
	for _, c := range rpcResp.Result.Cids {
		parsed, err := cid.Decode(c.Root)
		if err != nil {
			debugLog("[chain-head] bad CID %q from %s: %v", c.Root, targetName, err)
			return nil
		}
		cids = append(cids, parsed)
	}

	info := &chainHeadInfo{
		CIDs:   cids,
		Height: uint64(rpcResp.Result.Height),
	}
	debugLog("[chain-head] %s: height=%d cids=%d", targetName, info.Height, len(info.CIDs))
	return info
}

// discoverGenesisCID fetches the genesis CID from a Lotus node's RPC endpoint.
// Uses unauthenticated HTTP POST to Filecoin.ChainGetGenesis.
func discoverGenesisCID(rpcURL string) string {
	type rpcRequest struct {
		Jsonrpc string `json:"jsonrpc"`
		Method  string `json:"method"`
		Params  []any  `json:"params"`
		ID      int    `json:"id"`
	}
	type rpcResponse struct {
		Result struct {
			Cids []struct {
				Root string `json:"/"`
			} `json:"Cids"`
		} `json:"result"`
	}

	reqBody, _ := json.Marshal(rpcRequest{
		Jsonrpc: "2.0",
		Method:  "Filecoin.ChainGetGenesis",
		Params:  []any{},
		ID:      1,
	})

	deadline := time.Now().Add(discoveryTimeout)
	for time.Now().Before(deadline) {
		resp, err := http.Post(rpcURL, "application/json", bytes.NewReader(reqBody))
		if err != nil {
			log.Printf("[protocol-fuzzer] genesis CID fetch failed: %v, retrying...", err)
			time.Sleep(discoveryRetryInterval)
			continue
		}

		var rpcResp rpcResponse
		if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
			resp.Body.Close()
			log.Printf("[protocol-fuzzer] genesis CID decode failed: %v, retrying...", err)
			time.Sleep(discoveryRetryInterval)
			continue
		}
		resp.Body.Close()

		if len(rpcResp.Result.Cids) > 0 {
			genCID := rpcResp.Result.Cids[0].Root
			log.Printf("[protocol-fuzzer] genesis CID: %s", genCID)
			return genCID
		}

		log.Printf("[protocol-fuzzer] genesis CID empty, retrying...")
		time.Sleep(discoveryRetryInterval)
	}

	log.Fatal("[protocol-fuzzer] FATAL: cannot discover genesis CID")
	return ""
}
