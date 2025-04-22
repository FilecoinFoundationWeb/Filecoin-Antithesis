package resources

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"strings"
	"time"

	"github.com/antithesishq/antithesis-sdk-go/assert"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/client"
)

type NodeConfig struct {
	Name          string `json:"name"`
	RPCURL        string `json:"rpcURL"`
	AuthTokenPath string `json:"authTokenPath"`
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
			if err := verifyConnection(ctx, api, nodeConfig); err == nil {
				log.Printf("[INFO] Successfully connected to node %s on attempt %d", nodeConfig.Name, attempt)
				return api, closer, nil
			}
			closer()
			err = fmt.Errorf("connection verification failed")
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
	assert.Sometimes(err == nil, "RPC connection established", map[string]any{
		"node":  nodeConfig.Name,
		"url":   nodeConfig.RPCURL,
		"error": err,
	})
	return api, closer, err
}

// verifyConnection performs health checks on the connection
func verifyConnection(ctx context.Context, nodeAPI api.FullNode, nodeConfig NodeConfig) error {
	// Check if we can get version info
	_, err := nodeAPI.Version(ctx)
	if err != nil {
		log.Printf("[ERROR] Node %s version check failed: %v", nodeConfig.Name, err)
		return err
	}

	// Check if we can get chain head
	head, err := nodeAPI.ChainHead(ctx)
	if err != nil {
		log.Printf("[ERROR] Node %s chain head check failed: %v", nodeConfig.Name, err)
		return err
	}
	if head == nil {
		log.Printf("[ERROR] Node %s chain head is nil", nodeConfig.Name)
		return fmt.Errorf("chain head is nil")
	}

	// Check if node has peers
	_, err = nodeAPI.NetPeers(ctx)
	if err != nil {
		log.Printf("[ERROR] Node %s peer list check failed: %v", nodeConfig.Name, err)
		return err
	}

	return nil
}

func IsNodeConnected(ctx context.Context, nodeAPI api.FullNode) (bool, error) {
	peers, err := nodeAPI.NetPeers(ctx)
	assert.Sometimes(err == nil, "Peer list retrieval", map[string]any{
		"error":     err,
		"peerCount": len(peers),
	})
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
	assert.Sometimes(err == nil, "Peer connection", map[string]any{
		"source": currentNodeConfig.Name,
		"target": targetNodeConfig.Name,
		"peer":   otherPeerInfo.ID.String(),
		"error":  err,
	})

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
