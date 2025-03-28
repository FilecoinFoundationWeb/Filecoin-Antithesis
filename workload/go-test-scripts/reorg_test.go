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

	api1, closer1, _ := resources.ConnectToNode(ctx, *node1)
	defer closer1()
	api2, closer2, _ := resources.ConnectToNode(ctx, *node2)
	defer closer2()

	head1, _ := api1.ChainHead(ctx)
	head2, _ := api2.ChainHead(ctx)
	t.Logf("Initial heads: Lotus1=%d, Lotus2=%d", head1.Height(), head2.Height())

	pidStr := readFile(t, "/root/devgen/lotus-2/p2pID")
	peerID, _ := peer.Decode(pidStr)
	api1.NetDisconnect(ctx, peerID)

	time.Sleep(180 * time.Second) // allow divergence

	addrStr := readFile(t, "/root/devgen/lotus-2/lotus-2-ipv4addr")
	maddr, _ := ma.NewMultiaddr(addrStr)
	addrInfo, _ := peer.AddrInfoFromP2pAddr(maddr)
	api1.NetConnect(ctx, *addrInfo)

	time.Sleep(30 * time.Second) // wait for sync

	final1, _ := api1.ChainHead(ctx)
	final2, _ := api2.ChainHead(ctx)
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
