package chain

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/filecoin-project/go-jsonrpc"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/client"
)

// NodeConfig holds the configuration for connecting to Filecoin nodes.
type NodeConfig struct {
	Names []string // Node hostnames (e.g. ["lotus0", "lotus1"])
	Port  string   // RPC port (e.g. "1234")
}

// NewFilecoinClient creates an authenticated JSON-RPC client for a Filecoin node.
func NewFilecoinClient(ctx context.Context, addr string, token string) (api.FullNode, jsonrpc.ClientCloser, error) {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+token)
	return client.NewFullNodeRPCV1(ctx, addr, header)
}

// ConnectNodes connects to all configured Filecoin nodes.
// Returns connected nodes map, ordered key list, or error if no nodes connected.
func ConnectNodes(ctx context.Context, cfg NodeConfig) (map[string]api.FullNode, []string, error) {
	nodes := make(map[string]api.FullNode)
	var keys []string

	for _, name := range cfg.Names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}

		addr := fmt.Sprintf("ws://%s:%s/rpc/v1", name, cfg.Port)

		// JWT token path: /root/devgen/<nodename>/<nodename>-jwt
		tokenPath := fmt.Sprintf("/root/devgen/%s/%s-jwt", name, name)
		tokenBytes, err := os.ReadFile(tokenPath)
		if err != nil {
			log.Printf("[chain] WARN: no JWT at %s for node %s, trying without auth", tokenPath, name)
			tokenBytes = []byte("")
		}
		token := strings.TrimSpace(string(tokenBytes))

		node, closer, err := NewFilecoinClient(ctx, addr, token)
		if err != nil {
			log.Printf("[chain] ERROR: cannot connect to %s at %s: %v", name, addr, err)
			continue
		}
		_ = closer // keep connection open for engine lifetime

		nodes[name] = node
		keys = append(keys, name)
		log.Printf("[chain] connected to node %s at %s", name, addr)
	}

	if len(nodes) == 0 {
		return nil, nil, fmt.Errorf("no nodes connected")
	}
	log.Printf("[chain] connected to %d node(s): %v", len(nodes), keys)
	return nodes, keys, nil
}
