package resources

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/filecoin-project/go-jsonrpc"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/client"
	"github.com/filecoin-project/lotus/api/v2api"
	"github.com/filecoin-project/lotus/chain/types"
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

func LoadConfig(filename string) (*Config, error) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	var config Config
	err = json.Unmarshal(data, &config)
	return &config, err
}

func ConnectToNode(ctx context.Context, nodeConfig NodeConfig) (api.FullNode, func(), error) {
	var (
		api    api.FullNode
		closer func()
		err    error
	)

	for attempt := 1; attempt <= maxRetries; attempt++ {
		api, closer, err = tryConnect(ctx, nodeConfig)
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
				return nil, nil, fmt.Errorf("context cancelled while connecting to node %s: %w", nodeConfig.Name, ctx.Err())
			case <-time.After(waitTime):
			}
		}
	}

	return nil, nil, fmt.Errorf("failed to connect to node %s after %d attempts: %v", nodeConfig.Name, maxRetries, err)
}

// tryConnect attempts a single connection to the node
func tryConnect(ctx context.Context, nodeConfig NodeConfig) (api.FullNode, func(), error) {
	authToken, err := ioutil.ReadFile(nodeConfig.AuthTokenPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read auth token for node %s: %v", nodeConfig.Name, err)
	}
	finalAuthToken := strings.TrimSpace(string(authToken))
	headers := map[string][]string{"Authorization": {"Bearer " + finalAuthToken}}
	api, closer, err := client.NewFullNodeRPCV1(ctx, nodeConfig.RPCURL, headers)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to node %s: %v", nodeConfig.Name, err)
	}
	return api, closer, err
}

func ConnectToNodeV2(ctx context.Context, nodeConfig NodeConfig) (v2api.FullNode, func(), error) {
	authToken, err := ioutil.ReadFile(nodeConfig.AuthTokenPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read auth token for node %s: %v", nodeConfig.Name, err)
	}
	finalAuthToken := strings.TrimSpace(string(authToken))
	headers := map[string][]string{"Authorization": {"Bearer " + finalAuthToken}}
	api, closer, err := NewFullNodeRPCV2(ctx, nodeConfig.RPCURL, headers)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to node %s: %v", nodeConfig.Name, err)
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

func IsNodeConnected(ctx context.Context, nodeAPI api.FullNode) (bool, error) {
	peers, err := nodeAPI.NetPeers(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to get peer list: %w", err)
	}
	return len(peers) > 0, nil
}

func EnsureNodesConnected(ctx context.Context, currentNodeAPI api.FullNode, currentNodeConfig NodeConfig, allNodes []NodeConfig) (bool, error) {
	connected, err := IsNodeConnected(ctx, currentNodeAPI)
	if err != nil {
		return false, err
	}

	if connected {
		return true, nil
	}

	err = ConnectToOtherNodes(ctx, currentNodeAPI, currentNodeConfig, allNodes)
	if err != nil {
		return false, err
	}

	return IsNodeConnected(ctx, currentNodeAPI)
}

// ConnectToOtherNodes connects the current node to all other nodes in the config with retries.
func ConnectToOtherNodes(ctx context.Context, currentNodeAPI api.FullNode, currentNodeConfig NodeConfig, allNodes []NodeConfig) error {
	for _, nodeConfig := range allNodes {
		if nodeConfig.Name == currentNodeConfig.Name {
			continue
		}

		var err error
		for attempt := 1; attempt <= maxRetries; attempt++ {
			err = tryConnectToNode(ctx, currentNodeAPI, currentNodeConfig, nodeConfig)
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
					return fmt.Errorf("context cancelled while connecting nodes: %w", ctx.Err())
				case <-time.After(waitTime):
				}
			}
		}

		if err != nil {
			return fmt.Errorf("failed to connect node %s to %s after %d attempts: %v",
				currentNodeConfig.Name, nodeConfig.Name, maxRetries, err)
		}

		log.Printf("[INFO] Node %s successfully connected to node %s", currentNodeConfig.Name, nodeConfig.Name)
	}
	return nil
}

// tryConnectToNode attempts a single connection between two nodes
func tryConnectToNode(ctx context.Context, currentNodeAPI api.FullNode, currentNodeConfig NodeConfig, targetNodeConfig NodeConfig) error {
	otherNodeAPI, closer, err := ConnectToNode(ctx, targetNodeConfig)
	if err != nil {
		return fmt.Errorf("failed to connect to target node: %w", err)
	}
	defer closer()

	otherPeerInfo, err := otherNodeAPI.NetAddrsListen(ctx)
	if err != nil {
		return fmt.Errorf("failed to get peer info: %w", err)
	}

	err = currentNodeAPI.NetConnect(ctx, otherPeerInfo)
	if err != nil {
		return fmt.Errorf("failed to connect to peer: %w", err)
	}

	return err
}

// DisconnectFromOtherNodes disconnects the current node from all connected peers.
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

// ChainPredicate encapsulates a chain condition.
type ChainPredicate func(set *types.TipSet) bool

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
