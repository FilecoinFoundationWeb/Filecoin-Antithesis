package resources

import (
	"context"
	"fmt"
	"net/http"

	"github.com/filecoin-project/go-jsonrpc"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/client"
)

// Client wraps a connection to a Filecoin node.
type Client struct {
	Node   *Node
	API    api.FullNode
	closer jsonrpc.ClientCloser
}

// NewClient creates a new API client for a node.
func NewClient(ctx context.Context, node *Node) (*Client, error) {
	token, err := node.ReadToken()
	if err != nil {
		return nil, fmt.Errorf("failed to read token for %s: %w", node.ID, err)
	}

	headers := http.Header{}
	if token != "" {
		headers.Add("Authorization", "Bearer "+token)
	}

	api, closer, err := client.NewFullNodeRPCV1(ctx, node.RPC, headers)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to %s at %s: %w", node.ID, node.RPC, err)
	}

	return &Client{
		Node:   node,
		API:    api,
		closer: closer,
	}, nil
}

// Close closes the client connection.
func (c *Client) Close() {
	if c.closer != nil {
		c.closer()
	}
}

// ClientPool manages connections to multiple nodes.
type ClientPool struct {
	clients map[string]*Client
}

// NewClientPool creates connections to all nodes in the config.
func NewClientPool(ctx context.Context, cfg *Config) (*ClientPool, error) {
	pool := &ClientPool{
		clients: make(map[string]*Client),
	}

	for i := range cfg.Nodes {
		node := &cfg.Nodes[i]
		c, err := NewClient(ctx, node)
		if err != nil {
			pool.Close() // Clean up any already-created clients
			return nil, fmt.Errorf("failed to create client for %s: %w", node.ID, err)
		}
		pool.clients[node.ID] = c
	}

	return pool, nil
}

// Get returns a client by node ID.
func (p *ClientPool) Get(nodeID string) *Client {
	return p.clients[nodeID]
}

// All returns all clients.
func (p *ClientPool) All() []*Client {
	clients := make([]*Client, 0, len(p.clients))
	for _, c := range p.clients {
		clients = append(clients, c)
	}
	return clients
}

// Close closes all client connections.
func (p *ClientPool) Close() {
	for _, c := range p.clients {
		c.Close()
	}
}
