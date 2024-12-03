package resources

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"strings"

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
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to node %s: %v", nodeConfig.Name, err)
	}
	return api, closer, nil
}

// ConnectToOtherNodes connects the current node to all other nodes in the config.
func ConnectToOtherNodes(ctx context.Context, currentNodeAPI api.FullNode, currentNodeConfig NodeConfig, allNodes []NodeConfig) error {
	for _, nodeConfig := range allNodes {
		// Skip the current node
		if nodeConfig.Name == currentNodeConfig.Name {
			continue
		}

		// Connect to the other node
		otherNodeAPI, closer, err := ConnectToNode(ctx, nodeConfig)
		if err != nil {
			return fmt.Errorf("failed to connect to node %s: %v", nodeConfig.Name, err)
		}
		defer closer()

		// Retrieve the peer info for the other node
		otherPeerInfo, err := otherNodeAPI.NetAddrsListen(ctx)
		if err != nil {
			return fmt.Errorf("failed to get peer info for node %s: %v", nodeConfig.Name, err)
		}

		// Attempt to connect the current node to the other node using AddrInfo
		err = currentNodeAPI.NetConnect(ctx, otherPeerInfo)
		if err != nil {
			return fmt.Errorf("failed to connect node %s to node %s: %v", currentNodeConfig.Name, nodeConfig.Name, err)
		}
		fmt.Printf("Node %s connected to node %s\n", currentNodeConfig.Name, nodeConfig.Name)
	}
	return nil
}

// DisconnectFromOtherNodes disconnects the current node from all connected peers.
func DisconnectFromOtherNodes(ctx context.Context, currentNodeAPI api.FullNode) error {
	// Retrieve all connected peers
	peers, err := currentNodeAPI.NetPeers(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connected peers: %v", err)
	}

	// Disconnect from each peer
	for _, peer := range peers {
		err := currentNodeAPI.NetDisconnect(ctx, peer.ID)
		if err != nil {
			return fmt.Errorf("failed to disconnect from peer %s: %v", peer.ID.String(), err)
		}
		fmt.Printf("Disconnected from peer %s\n", peer.ID.String())
	}
	return nil
}

