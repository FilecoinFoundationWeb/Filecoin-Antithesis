package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources"
	"github.com/antithesishq/antithesis-sdk-go/assert"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

func TestReorgAfterDisconnectReconnect(t *testing.T) {
	ctx := context.Background()

	cfg, err := resources.LoadConfig("/opt/antithesis/resources/config.json")
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	var node1, node2 *resources.NodeConfig
	for i := range cfg.Nodes {
		if cfg.Nodes[i].Name == "Lotus1" {
			node1 = &cfg.Nodes[i]
		}
		if cfg.Nodes[i].Name == "Lotus2" {
			node2 = &cfg.Nodes[i]
		}
	}
	if node1 == nil || node2 == nil {
		t.Fatal("Missing node definitions for Lotus1 or Lotus2")
	}

	api1, closer1, err := resources.ConnectToNode(ctx, *node1)
	if err != nil {
		t.Fatalf("failed to connect to Lotus1: %v", err)
	}
	defer closer1()

	api2, closer2, err := resources.ConnectToNode(ctx, *node2)
	if err != nil {
		t.Fatalf("failed to connect to Lotus2: %v", err)
	}
	defer closer2()

	head1, err := api1.ChainHead(ctx)
	if err != nil || head1 == nil {
		t.Fatalf("failed to get chain head for Lotus1: %v", err)
	}
	head2, err := api2.ChainHead(ctx)
	if err != nil || head2 == nil {
		t.Fatalf("failed to get chain head for Lotus2: %v", err)
	}
	t.Logf("Initial heads: Lotus1=%d, Lotus2=%d", head1.Height(), head2.Height())

	pidStr := readFile(t, "/root/devgen/lotus-2/p2pID")
	peerID, err := peer.Decode(pidStr)
	if err != nil {
		t.Fatalf("failed to decode peer ID: %v", err)
	}
	if err := api1.NetDisconnect(ctx, peerID); err != nil {
		t.Fatalf("failed to disconnect Lotus1 from Lotus2: %v", err)
	}

	time.Sleep(180 * time.Second)

	addrStr := readFile(t, "/root/devgen/lotus-2/lotus-2-ipv4addr")
	maddr, err := ma.NewMultiaddr(addrStr)
	if err != nil {
		t.Fatalf("failed to parse multiaddr: %v", err)
	}
	addrInfo, err := peer.AddrInfoFromP2pAddr(maddr)
	if err != nil {
		t.Fatalf("failed to convert to AddrInfo: %v", err)
	}
	if err := api1.NetConnect(ctx, *addrInfo); err != nil {
		t.Fatalf("failed to reconnect Lotus1 to Lotus2: %v", err)
	}

	time.Sleep(30 * time.Second) // wait for sync

	final1, err := api1.ChainHead(ctx)
	if err != nil || final1 == nil {
		t.Fatalf("failed to get final chain head for Lotus1: %v", err)
	}
	final2, err := api2.ChainHead(ctx)
	if err != nil || final2 == nil {
		t.Fatalf("failed to get final chain head for Lotus2: %v", err)
	}
	t.Logf("Final heads: Lotus1=%d, Lotus2=%d", final1.Height(), final2.Height())

	assert.Always(final1.Key() == final2.Key(), "Chains must match after reconnect", map[string]interface{}{
		"lotus1": final1.Key().String(),
		"lotus2": final2.Key().String(),
	})
}

func readFile(t *testing.T, path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}
	return strings.TrimSpace(string(data))
}
