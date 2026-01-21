package resources

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"strings"
	"time"

	"github.com/filecoin-project/go-jsonrpc"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/client"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
)

type NodeConfig struct {
	Name          string `json:"name"`
	RPCURL        string `json:"rpcurl"`
	AuthTokenPath string `json:"authtokenpath"`
}

type Config struct {
	Nodes                []NodeConfig `json:"nodes"`
	DefaultFundingAmount string       `json:"defaultFundingAmount"`
}

const (
	maxRetries           = 3
	connectionRetryDelay = 5 * time.Second
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

// RetryOperation executes an operation with simple retry logic
func RetryOperation(ctx context.Context, operation func() error, operationName string) error {
	var err error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		err = operation()
		if err == nil {
			return nil
		}

		if attempt < maxRetries {
			log.Printf("[WARN] %s failed (attempt %d/%d): %v. Retrying in %v...",
				operationName, attempt, maxRetries, err, connectionRetryDelay)

			select {
			case <-ctx.Done():
				log.Printf("[ERROR] Context cancelled during %s: %v", operationName, ctx.Err())
				return ctx.Err()
			case <-time.After(connectionRetryDelay):
			}
		}
	}

	log.Printf("[ERROR] %s failed after %d attempts: %v", operationName, maxRetries, err)
	return err
}

// ConnectToNode establishes a connection to a Filecoin node with retry logic
func ConnectToNode(ctx context.Context, nodeConfig NodeConfig) (api.FullNode, func(), error) {
	var (
		res    api.FullNode
		closer func()
		err    error
	)

	opErr := RetryOperation(ctx, func() error {
		res, closer, err = ConnectToNodeV1(ctx, nodeConfig)
		return err
	}, fmt.Sprintf("Connect to node %s", nodeConfig.Name))

	if opErr != nil {
		return nil, nil, opErr
	}

	return res, closer, nil
}

// ConnectToNodeV1 attempts a single connection to the node using the V1 RPC API
func ConnectToNodeV1(ctx context.Context, nodeConfig NodeConfig) (api.FullNode, func(), error) {
	authToken, err := ioutil.ReadFile(nodeConfig.AuthTokenPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read auth token: %w", err)
	}
	finalAuthToken := strings.TrimSpace(string(authToken))
	headers := map[string][]string{"Authorization": {"Bearer " + finalAuthToken}}

	api, closer, err := client.NewFullNodeRPCV1(ctx, nodeConfig.RPCURL, headers)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create RPC client: %w", err)
	}
	return api, closer, nil
}

// ConnectToCommonNode establishes a connection using the CommonAPI struct
func ConnectToCommonNode(ctx context.Context, nodeConfig NodeConfig) (CommonAPI, func(), error) {
	authToken, err := ioutil.ReadFile(nodeConfig.AuthTokenPath)
	if err != nil {
		return CommonAPI{}, nil, fmt.Errorf("failed to read auth token: %w", err)
	}
	finalAuthToken := strings.TrimSpace(string(authToken))
	headers := map[string][]string{"Authorization": {"Bearer " + finalAuthToken}}

	var res CommonAPI
	closer, err := jsonrpc.NewMergeClient(ctx, nodeConfig.RPCURL, "Filecoin",
		[]interface{}{&res}, headers)

	if err != nil {
		return CommonAPI{}, nil, fmt.Errorf("failed to create common RPC client: %w", err)
	}
	return res, closer, nil
}

// ConnectToOtherNodes connects the current node to all other nodes in the config with retries
func ConnectToOtherNodes(ctx context.Context, currentNodeAPI api.FullNode, currentNodeConfig NodeConfig, allNodes []NodeConfig) error {
	for _, nodeConfig := range allNodes {
		if nodeConfig.Name == currentNodeConfig.Name {
			continue
		}

		err := RetryOperation(ctx, func() error {
			return tryConnectToNode(ctx, currentNodeAPI, nodeConfig)
		}, fmt.Sprintf("Connect node %s to %s", currentNodeConfig.Name, nodeConfig.Name))

		if err != nil {
			log.Printf("[ERROR] Failed to connect node %s to %s: %v", currentNodeConfig.Name, nodeConfig.Name, err)
			continue
		}

		log.Printf("[INFO] Node %s successfully connected to node %s", currentNodeConfig.Name, nodeConfig.Name)
	}
	return nil
}

// tryConnectToNode attempts a single connection between two nodes
func tryConnectToNode(ctx context.Context, currentNodeAPI api.FullNode, targetNodeConfig NodeConfig) error {
	otherNodeAPI, closer, err := ConnectToNode(ctx, targetNodeConfig)
	if err != nil {
		return err
	}
	defer closer()

	otherPeerInfo, err := otherNodeAPI.NetAddrsListen(ctx)
	if err != nil {
		return fmt.Errorf("failed to get peer info: %w", err)
	}

	if err := currentNodeAPI.NetConnect(ctx, otherPeerInfo); err != nil {
		return fmt.Errorf("failed to connect to peer: %w", err)
	}

	return nil
}

// DisconnectFromOtherNodes disconnects the current node from all connected peers
func DisconnectFromOtherNodes(ctx context.Context, nodeAPI api.FullNode) error {
	peers, err := nodeAPI.NetPeers(ctx)
	if err != nil {
		return fmt.Errorf("failed to get peer list: %w", err)
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
		return fmt.Errorf("failed to get peer list: %w", err)
	}

	peerMultiaddrs := make(map[peer.ID][]multiaddr.Multiaddr)
	for _, p := range peers {
		peerMultiaddrs[p.ID] = p.Addrs
		if err := nodeAPI.NetDisconnect(ctx, p.ID); err != nil {
			log.Printf("[WARN] Failed to disconnect from peer %s: %v", p.ID.String(), err)
		}
	}

	reconnectDelay := 5 * time.Minute
	log.Printf("[INFO] Waiting %v before reconnecting...", reconnectDelay)

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(reconnectDelay):
	}

	for peerID, addrs := range peerMultiaddrs {
		addrInfo := peer.AddrInfo{ID: peerID, Addrs: addrs}
		if err := nodeAPI.NetConnect(ctx, addrInfo); err != nil {
			log.Printf("[WARN] Failed to reconnect to peer %s: %v", peerID.String(), err)
		}
	}

	return nil
}

// FilterV1Nodes returns all nodes (legacy name, maintained for compatibility)
func FilterV1Nodes(nodes []NodeConfig) []NodeConfig {
	return nodes
}

// FilterLotusNodes returns only nodes with names starting with "Lotus"
func FilterLotusNodes(nodes []NodeConfig) []NodeConfig {
	var filtered []NodeConfig
	for _, n := range nodes {
		if strings.HasPrefix(n.Name, "Lotus") {
			filtered = append(filtered, n)
		}
	}
	return filtered
}

// FilterForestNodes returns only nodes with names starting with "Forest"
func FilterForestNodes(nodes []NodeConfig) []NodeConfig {
	var filtered []NodeConfig
	for _, n := range nodes {
		if strings.HasPrefix(n.Name, "Forest") {
			filtered = append(filtered, n)
		}
	}
	return filtered
}

// ChainPredicate encapsulates a chain condition.
type ChainPredicate func(set *types.TipSet) bool

// WaitTillChain waits for a chain condition specified by the predicate to be met
func WaitTillChain(ctx context.Context, api api.FullNode, pred ChainPredicate) *types.TipSet {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	heads, err := api.ChainNotify(ctx)
	if err != nil {
		log.Printf("[ERROR] ChainNotify failed: %v", err)
		return nil
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

// PerformConnectDisconnectOperation toggles connection for a node
func PerformConnectDisconnectOperation(ctx context.Context, nodeConfig *NodeConfig, config *Config) error {
	log.Printf("Toggling connection for node '%s'...", nodeConfig.Name)
	api, closer, err := ConnectToNode(ctx, *nodeConfig)
	if err != nil {
		log.Printf("[ERROR] Failed to connect to node '%s': %v", nodeConfig.Name, err)
		return err
	}
	defer closer()

	// Connect/Disconnect against ALL nodes? Or just Lotus?
	// Assuming all nodes for connectivity stress.
	allNodes := FilterV1Nodes(config.Nodes)

	return RetryOperation(ctx, func() error {
		peers, err := api.NetPeers(ctx)
		if err != nil {
			return fmt.Errorf("failed to get peers: %w", err)
		}

		if len(peers) > 0 {
			log.Printf("Node '%s' has %d peers, disconnecting...", nodeConfig.Name, len(peers))
			if err := DisconnectFromOtherNodes(ctx, api); err != nil {
				return err
			}
			log.Printf("Node '%s' disconnected successfully", nodeConfig.Name)
		} else {
			log.Printf("Node '%s' has no peers, connecting...", nodeConfig.Name)
			if err := ConnectToOtherNodes(ctx, api, *nodeConfig, allNodes); err != nil {
				return err
			}
			log.Printf("Node '%s' connected successfully", nodeConfig.Name)
		}
		return nil
	}, fmt.Sprintf("Connection toggle for %s", nodeConfig.Name))
}

func PerformReorgOperation(ctx context.Context, nodeConfig *NodeConfig, checkConsensus bool) error {
	log.Printf("Simulating reorg for node '%s'...", nodeConfig.Name)
	api, closer, err := ConnectToNode(ctx, *nodeConfig)
	if err != nil {
		log.Printf("[ERROR] Failed to connect to node '%s': %v", nodeConfig.Name, err)
		return err
	}
	defer closer()

	if err := SimulateReorg(ctx, api); err != nil {
		log.Printf("[ERROR] Failed to simulate reorg for node '%s': %v", nodeConfig.Name, err)
		return err
	}
	log.Printf("Reorg simulation completed successfully for node '%s'", nodeConfig.Name)
	return nil
}
