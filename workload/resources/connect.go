package resources

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"strings"

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

// LoadConfig reads a JSON configuration file and unmarshals it into a Config struct.
func LoadConfig(filename string) (*Config, error) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	var config Config
	err = json.Unmarshal(data, &config)
	return &config, err
}

// ConnectToNode connects to a Lotus node using the provided configuration.
func ConnectToNode(ctx context.Context, nodeConfig NodeConfig) (api.FullNode, func(), error) {
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
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to node %s: %v", nodeConfig.Name, err)
	}

	version, err := api.Version(ctx)
	assert.Sometimes(err == nil, "API responsive", map[string]any{
		"node":    nodeConfig.Name,
		"error":   err,
		"version": version,
	})

	return api, closer, nil
}

// IsNodeConnected checks if the current node has any peers connected.
// Returns true if connected to at least one peer, false otherwise.
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

// ConnectToOtherNodes connects the current node to all other nodes in the config.
func ConnectToOtherNodes(ctx context.Context, currentNodeAPI api.FullNode, currentNodeConfig NodeConfig, allNodes []NodeConfig) error {
	for _, nodeConfig := range allNodes {
		if nodeConfig.Name == currentNodeConfig.Name {
			continue
		}

		otherNodeAPI, closer, err := ConnectToNode(ctx, nodeConfig)
		if err != nil {
			return fmt.Errorf("failed to connect to node %s: %v", nodeConfig.Name, err)
		}
		defer closer()

		otherPeerInfo, err := otherNodeAPI.NetAddrsListen(ctx)
		if err != nil {
			return fmt.Errorf("failed to get peer info for node %s: %v", nodeConfig.Name, err)
		}

		err = currentNodeAPI.NetConnect(ctx, otherPeerInfo)
		assert.Sometimes(err == nil, "Peer connection", map[string]any{
			"source": currentNodeConfig.Name,
			"target": nodeConfig.Name,
			"peer":   otherPeerInfo.ID.String(),
			"error":  err,
		})
		if err != nil {
			return fmt.Errorf("failed to connect node %s to node %s: %v", currentNodeConfig.Name, nodeConfig.Name, err)
		}
		fmt.Printf("Node %s connected to node %s\n", currentNodeConfig.Name, nodeConfig.Name)
	}
	return nil
}

// DisconnectFromOtherNodes disconnects the current node from all connected peers.
func DisconnectFromOtherNodes(ctx context.Context, currentNodeAPI api.FullNode) error {
	peers, err := currentNodeAPI.NetPeers(ctx)
	assert.Sometimes(err == nil, "Peer list retrieval for disconnect", map[string]any{
		"error":     err,
		"peerCount": len(peers),
	})
	if err != nil {
		return fmt.Errorf("failed to get connected peers: %v", err)
	}

	for _, peer := range peers {
		err := currentNodeAPI.NetDisconnect(ctx, peer.ID)
		assert.Sometimes(err == nil, "Peer disconnect", map[string]any{
			"peer":  peer.ID.String(),
			"error": err,
		})
		if err != nil {
			return fmt.Errorf("failed to disconnect from peer %s: %v", peer.ID.String(), err)
		}
		fmt.Printf("Disconnected from peer %s\n", peer.ID.String())
	}
	return nil
}
