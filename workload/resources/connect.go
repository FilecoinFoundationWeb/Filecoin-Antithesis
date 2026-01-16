package resources

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/filecoin-project/go-jsonrpc"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/client"
	"github.com/filecoin-project/lotus/api/v2api"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
)

type NodeConfig struct {
	Name          string `json:"name"`
	RPCURL        string `json:"rpcURL"`
	AuthTokenPath string `json:"authTokenPath"`
	Type          string `json:"type,omitempty"` // "lotus" or "forest"
}

type Config struct {
	Nodes                []NodeConfig `json:"nodes"`
	DefaultFundingAmount string       `json:"defaultFundingAmount"`
}

const (
	maxRetries        = 3                // Fewer retries but longer waits
	initialRetryDelay = 10 * time.Second // Start with a longer initial delay
	maxRetryDelay     = 40 * time.Second // Max delay slightly longer than crash recovery
	minRecoveryTime   = 30 * time.Second // Minimum time to wait for node recovery
)

// LoadConfig reads and parses a JSON configuration file containing node information
func LoadConfig(filename string) (*Config, error) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	var config Config
	err = json.Unmarshal(data, &config)
	return &config, err
}

// RetryOperation executes an operation with retry logic
func RetryOperation(ctx context.Context, operation func() error, operationName string) error {
	var err error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		err = operation()
		if err == nil {
			return nil
		}

		if attempt < maxRetries {
			waitTime := minRecoveryTime
			if attempt > 1 {
				waitTime = initialRetryDelay * time.Duration(attempt)
				if waitTime > maxRetryDelay {
					waitTime = maxRetryDelay
				}
			}

			log.Printf("[WARN] %s failed (attempt %d/%d): %v. Waiting %v before retry...",
				operationName, attempt, maxRetries, err, waitTime)

			select {
			case <-ctx.Done():
				log.Printf("[ERROR] Context cancelled during %s: %v", operationName, ctx.Err())
				return nil
			case <-time.After(waitTime):
			}
		}
	}

	log.Printf("[ERROR] %s failed after %d attempts: %v", operationName, maxRetries, err)
	return nil
}

// ConnectToNode establishes a connection to a Filecoin node with retry logic
// It will attempt to connect maxRetries times with increasing delay between attempts
func ConnectToNode(ctx context.Context, nodeConfig NodeConfig) (api.FullNode, func(), error) {
	var (
		api    api.FullNode
		closer func()
		err    error
	)

	for attempt := 1; attempt <= maxRetries; attempt++ {
		api, closer, err = ConnectToNodeV1(ctx, nodeConfig)
		if err == nil {
			return api, closer, nil
		}

		if attempt < maxRetries {
			waitTime := minRecoveryTime
			if attempt > 1 {
				waitTime = initialRetryDelay * time.Duration(attempt)
				if waitTime > maxRetryDelay {
					waitTime = maxRetryDelay
				}
			}

			log.Printf("[WARN] Failed to connect to node %s (attempt %d/%d): %v. Waiting %v for node recovery...",
				nodeConfig.Name, attempt, maxRetries, err, waitTime)

			select {
			case <-ctx.Done():
				log.Printf("[INFO] Context cancelled while connecting to node %s", nodeConfig.Name)
				return nil, nil, nil
			case <-time.After(waitTime):
			}
		}
	}

	log.Printf("[ERROR] Failed to connect to node %s after %d attempts: %v", nodeConfig.Name, maxRetries, err)
	return nil, nil, nil
}

// ConnectToNodeV1 attempts a single connection to the node using the V1 RPC API
func ConnectToNodeV1(ctx context.Context, nodeConfig NodeConfig) (api.FullNode, func(), error) {
	authToken, err := ioutil.ReadFile(nodeConfig.AuthTokenPath)
	if err != nil {
		log.Printf("[ERROR] Failed to read auth token for node %s: %v", nodeConfig.Name, err)
		return nil, nil, nil
	}
	finalAuthToken := strings.TrimSpace(string(authToken))
	headers := map[string][]string{"Authorization": {"Bearer " + finalAuthToken}}
	api, closer, err := client.NewFullNodeRPCV1(ctx, nodeConfig.RPCURL, headers)
	if err != nil {
		log.Printf("[ERROR] Failed to connect to node %s: %v", nodeConfig.Name, err)
		return nil, nil, nil
	}
	return api, closer, err
}

// ConnectToNodeV2 establishes a connection to a Filecoin node using the V2 RPC API
func ConnectToNodeV2(ctx context.Context, nodeConfig NodeConfig) (v2api.FullNode, func(), error) {
	authToken, err := ioutil.ReadFile(nodeConfig.AuthTokenPath)
	if err != nil {
		log.Printf("[ERROR] Failed to read auth token for node %s: %v", nodeConfig.Name, err)
		return nil, nil, nil
	}
	finalAuthToken := strings.TrimSpace(string(authToken))
	headers := map[string][]string{"Authorization": {"Bearer " + finalAuthToken}}
	api, closer, err := NewFullNodeRPCV2(ctx, nodeConfig.RPCURL, headers)
	if err != nil {
		log.Printf("[ERROR] Failed to connect to node %s: %v", nodeConfig.Name, err)
		return nil, nil, nil
	}
	return api, closer, err
}

// NewFullNodeRPCV2 creates a new http jsonrpc client for the /v2 API.
func NewFullNodeRPCV2(ctx context.Context, addr string, requestHeader http.Header, opts ...jsonrpc.Option) (v2api.FullNode, jsonrpc.ClientCloser, error) {
	var res v2api.FullNodeStruct
	closer, err := jsonrpc.NewMergeClient(ctx, addr, "Filecoin",
		api.GetInternalStructs(&res), requestHeader, append([]jsonrpc.Option{jsonrpc.WithErrors(api.RPCErrors)}, opts...)...)

	return &res, closer, err
}

// ConnectToOtherNodes connects the current node to all other nodes in the config with retries
func ConnectToOtherNodes(ctx context.Context, currentNodeAPI api.FullNode, currentNodeConfig NodeConfig, allNodes []NodeConfig) error {
	for _, nodeConfig := range allNodes {
		if nodeConfig.Name == currentNodeConfig.Name {
			continue
		}

		var err error
		for attempt := 1; attempt <= maxRetries; attempt++ {
			err = tryConnectToNode(ctx, currentNodeAPI, nodeConfig)
			if err == nil {
				break
			}

			if attempt < maxRetries {
				// On first failure, wait at least the minimum recovery time
				waitTime := minRecoveryTime
				if attempt > 1 {
					waitTime = initialRetryDelay * time.Duration(attempt)
					if waitTime > maxRetryDelay {
						waitTime = maxRetryDelay
					}
				}

				log.Printf("[WARN] Failed to connect node %s to %s (attempt %d/%d): %v. Waiting %v for node recovery...",
					currentNodeConfig.Name, nodeConfig.Name, attempt, maxRetries, err, waitTime)

				select {
				case <-ctx.Done():
					log.Printf("[ERROR] Context cancelled while connecting nodes: %v", ctx.Err())
					return nil
				case <-time.After(waitTime):
				}
			}
		}

		if err != nil {
			log.Printf("[ERROR] Failed to connect node %s to %s after %d attempts: %v",
				currentNodeConfig.Name, nodeConfig.Name, maxRetries, err)
		}

		log.Printf("[INFO] Node %s successfully connected to node %s", currentNodeConfig.Name, nodeConfig.Name)
	}
	return nil
}

// tryConnectToNode attempts a single connection between two nodes
func tryConnectToNode(ctx context.Context, currentNodeAPI api.FullNode, targetNodeConfig NodeConfig) error {
	otherNodeAPI, closer, err := ConnectToNode(ctx, targetNodeConfig)
	if err != nil {
		log.Printf("[ERROR] Failed to connect to target node: %v", err)
		return nil
	}
	defer closer()

	otherPeerInfo, err := otherNodeAPI.NetAddrsListen(ctx)
	if err != nil {
		log.Printf("[ERROR] Failed to get peer info: %v", err)
		return nil
	}

	err = currentNodeAPI.NetConnect(ctx, otherPeerInfo)
	if err != nil {
		log.Printf("[ERROR] Failed to connect to peer: %v", err)
		return nil
	}

	return err
}

// DisconnectFromOtherNodes disconnects the current node from all connected peers
func DisconnectFromOtherNodes(ctx context.Context, nodeAPI api.FullNode) error {
	peers, err := nodeAPI.NetPeers(ctx)
	if err != nil {
		log.Printf("[ERROR] Failed to get peer list: %v", err)
		return nil
	}

	for _, peer := range peers {
		if err := nodeAPI.NetDisconnect(ctx, peer.ID); err != nil {
			log.Printf("[WARN] Failed to disconnect from peer %s: %v", peer.ID.String(), err)
		}
	}
	return nil
}

// SimulateReorg disconnects the current node from all connected peers,
// saves their multiaddrs, waits for a few minutes, and then reconnects
func SimulateReorg(ctx context.Context, nodeAPI api.FullNode) error {
	peers, err := nodeAPI.NetPeers(ctx)
	if err != nil {
		log.Printf("[ERROR] Failed to get peer list: %v", err)
		return nil
	}

	// Map to store peer IDs and their multiaddrs
	peerMultiaddrs := make(map[peer.ID][]multiaddr.Multiaddr)

	// Save multiaddrs for each peer before disconnecting
	for _, peer := range peers {
		log.Printf("[INFO] Saving multiaddrs for peer: %s", peer.ID.String())
		peerMultiaddrs[peer.ID] = peer.Addrs

		// Disconnect from the peer
		if err := nodeAPI.NetDisconnect(ctx, peer.ID); err != nil {
			log.Printf("[WARN] Failed to disconnect from peer %s: %v", peer.ID.String(), err)
		} else {
			log.Printf("[INFO] Successfully disconnected from peer: %s", peer.ID.String())
		}
	}

	// Wait for a few minutes before reconnecting
	reconnectDelay := 5 * time.Minute
	log.Printf("[INFO] Waiting %v before reconnecting to peers...", reconnectDelay)

	select {
	case <-ctx.Done():
		log.Printf("[ERROR] Context cancelled while waiting to reconnect: %v", ctx.Err())
		return nil
	case <-time.After(reconnectDelay):
	}

	// Reconnect to all peers using their saved multiaddrs
	log.Printf("[INFO] Reconnecting to %d peers...", len(peerMultiaddrs))
	for peerID, addrs := range peerMultiaddrs {
		// Create AddrInfo for the peer
		addrInfo := peer.AddrInfo{
			ID:    peerID,
			Addrs: addrs,
		}

		// Connect to the peer
		if err := nodeAPI.NetConnect(ctx, addrInfo); err != nil {
			log.Printf("[WARN] Failed to reconnect to peer %s: %v", peerID.String(), err)
		} else {
			log.Printf("[INFO] Successfully reconnected to peer: %s", peerID.String())
		}
	}

	return nil
}

// FilterLotusNodes returns a slice of NodeConfig containing only Lotus1 and Lotus2 nodes
func FilterLotusNodes(nodes []NodeConfig) []NodeConfig {
	var lotusNodes []NodeConfig
	for _, node := range nodes {
		if node.Name == "Lotus1" || node.Name == "Lotus2" {
			lotusNodes = append(lotusNodes, node)
		}
	}
	return lotusNodes
}

// FilterLotusNodes returns a slice of NodeConfig containing only Lotus1 and Lotus2 nodes
func FilterV1Nodes(nodes []NodeConfig) []NodeConfig {
	var lotusNodes []NodeConfig
	for _, node := range nodes {
		if node.Name == "Lotus1" || node.Name == "Lotus2" || node.Name == "Forest" {
			lotusNodes = append(lotusNodes, node)
		}
	}
	return lotusNodes
}

// ChainPredicate encapsulates a chain condition.
type ChainPredicate func(set *types.TipSet) bool

// WaitTillChain waits for a chain condition specified by the predicate to be met
// It monitors chain notifications and returns the tipset that satisfies the condition
func WaitTillChain(ctx context.Context, api api.FullNode, pred ChainPredicate) *types.TipSet {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	heads, err := api.ChainNotify(ctx)
	if err != nil {
		log.Printf("Cannot get head!!")
	}

	for chg := range heads {
		for _, c := range chg {
			if c.Type != "apply" {
				continue
			}
			if ts := c.Val; pred(ts) {
				return ts
			}
		}
	}
	return nil
}

// FilterLotusNodesV1 returns only V1 Lotus nodes (Lotus1 and Lotus2)
func FilterLotusNodesV1(nodes []NodeConfig) []NodeConfig {
	var filteredNodes []NodeConfig
	for _, node := range nodes {
		if node.Name == "Lotus1" || node.Name == "Lotus2" {
			filteredNodes = append(filteredNodes, node)
		}
	}
	return filteredNodes
}

// FilterLotusNodesWithV2 returns only V2 Lotus nodes (Lotus1-V2 and Lotus2-V2)
func FilterLotusNodesWithV2(nodes []NodeConfig) []NodeConfig {
	var filteredNodes []NodeConfig
	for _, node := range nodes {
		if node.Name == "Lotus1-V2" || node.Name == "Lotus2-V2" {
			filteredNodes = append(filteredNodes, node)
		}
	}
	return filteredNodes
}

// IsConsensusOrEthScriptRunning checks if any consensus or ETH-related scripts are currently running
// that require nodes to be connected for proper operation
func IsConsensusOrEthScriptRunning() (bool, error) {
	// List of consensus and ETH-related script patterns to check for
	consensusPatterns := []string{
		"consensus check",
		"consensus finalized",
		"monitor peers",
		"monitor f3",
		"chain backfill",
		"state check",
		"eth check",
		"anytime_f3_finalized_tipsets",
		"anytime_f3_running",
		"anytime_print_peer_info",
		"anytime_chain_backfill",
		"anytime_state_checks",
		"anytime_eth_methods_check",
		"parallel_driver_eth_legacy_tx",
	}

	// Check for running processes
	cmd := exec.Command("ps", "aux")
	output, err := cmd.Output()
	if err != nil {
		log.Printf("[ERROR] Failed to check running processes: %v", err)
		return false, nil
	}

	outputStr := strings.ToLower(string(output))

	for _, pattern := range consensusPatterns {
		if strings.Contains(outputStr, strings.ToLower(pattern)) {
			log.Printf("[WARN] Detected running consensus/eth script: %s", pattern)
			return true, nil
		}
	}

	return false, nil
}

// PerformConnectDisconnectOperation toggles connection for a node
func PerformConnectDisconnectOperation(ctx context.Context, nodeConfig *NodeConfig, config *Config) error {
	log.Printf("Toggling connection for node '%s'...", nodeConfig.Name)
	api, closer, err := ConnectToNode(ctx, *nodeConfig)
	if err != nil {
		log.Printf("[ERROR] Failed to connect to Lotus node '%s': %v", nodeConfig.Name, err)
		return nil
	}
	defer closer()

	var lotusNodes []NodeConfig
	for _, node := range config.Nodes {
		if node.Name == "Lotus1" || node.Name == "Lotus2" {
			lotusNodes = append(lotusNodes, node)
		}
	}

	return RetryOperation(ctx, func() error {
		// Check current connections
		peers, err := api.NetPeers(ctx)
		if err != nil {
			log.Printf("[ERROR] Failed to get peer list: %v", err)
			return nil
		}

		// If we have peers, disconnect; otherwise connect
		if len(peers) > 0 {
			log.Printf("Node '%s' has %d peers, disconnecting...", nodeConfig.Name, len(peers))
			if err := DisconnectFromOtherNodes(ctx, api); err != nil {
				log.Printf("[ERROR] Failed to disconnect node '%s' from other nodes: %v", nodeConfig.Name, err)
				return nil
			}
			log.Printf("Node '%s' disconnected successfully", nodeConfig.Name)
		} else {
			log.Printf("Node '%s' has no peers, connecting...", nodeConfig.Name)
			if err := ConnectToOtherNodes(ctx, api, *nodeConfig, lotusNodes); err != nil {
				log.Printf("[ERROR] Failed to connect node '%s' to other nodes: %v", nodeConfig.Name, err)
				return nil
			}
			log.Printf("Node '%s' connected successfully", nodeConfig.Name)
		}
		return nil
	}, fmt.Sprintf("Connection toggle operation for node %s", nodeConfig.Name))
}

// PerformReorgOperation simulates a reorg by disconnecting, waiting, and reconnecting
func PerformReorgOperation(ctx context.Context, nodeConfig *NodeConfig, checkConsensus bool) error {
	// Check for running consensus scripts if the flag is enabled
	if checkConsensus {
		isRunning, err := IsConsensusOrEthScriptRunning()
		if err != nil {
			log.Printf("[WARN] Failed to check for consensus/eth scripts: %v", err)
		} else if isRunning {
			log.Printf("[INFO] Consensus/ETH scripts detected running. Exiting reorg simulation early to avoid interference.")
			return nil
		}
	}

	log.Printf("Simulating reorg for node '%s'...", nodeConfig.Name)
	api, closer, err := ConnectToNode(ctx, *nodeConfig)
	if err != nil {
		log.Printf("[ERROR] Failed to connect to Lotus node '%s': %v", nodeConfig.Name, err)
		return nil
	}
	defer closer()

	if err := SimulateReorg(ctx, api); err != nil {
		log.Printf("failed to simulate reorg for node '%s': %v", nodeConfig.Name, err)
		return nil
	}
	log.Printf("Reorg simulation completed successfully for node '%s'", nodeConfig.Name)
	return nil
}
