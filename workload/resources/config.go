package resources

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Node represents a Filecoin node configuration.
type Node struct {
	ID             string `json:"id"`             // Unique identifier (e.g., "lotus0")
	RPC            string `json:"rpc"`            // RPC endpoint URL
	Token          string `json:"token"`          // Path to JWT token file (empty if none)
	Implementation string `json:"implementation"` // "lotus" or "forest"
	Role           string `json:"role"`           // "full", "miner", "bootstrap"
}

// Config holds the workload configuration.
type Config struct {
	Nodes []Node `json:"nodes"`
}

// LoadConfig reads and parses the config file.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	return &cfg, nil
}

// GetNode returns a node by ID, or nil if not found.
func (c *Config) GetNode(id string) *Node {
	for i := range c.Nodes {
		if c.Nodes[i].ID == id {
			return &c.Nodes[i]
		}
	}
	return nil
}

// GetNodesByImpl returns all nodes with the given implementation.
func (c *Config) GetNodesByImpl(impl string) []Node {
	var nodes []Node
	impl = strings.ToLower(impl)
	for _, n := range c.Nodes {
		if strings.ToLower(n.Implementation) == impl {
			nodes = append(nodes, n)
		}
	}
	return nodes
}

// GetFullNodes returns all nodes with role "full".
func (c *Config) GetFullNodes() []Node {
	var nodes []Node
	for _, n := range c.Nodes {
		if strings.ToLower(n.Role) == "full" {
			nodes = append(nodes, n)
		}
	}
	return nodes
}

// ReadToken reads the JWT token from the node's token file.
func (n *Node) ReadToken() (string, error) {
	if n.Token == "" {
		return "", nil
	}
	data, err := os.ReadFile(n.Token)
	if err != nil {
		return "", fmt.Errorf("failed to read token file %s: %w", n.Token, err)
	}
	return strings.TrimSpace(string(data)), nil
}
