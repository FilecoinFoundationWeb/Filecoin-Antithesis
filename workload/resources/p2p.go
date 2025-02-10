package resources

import (
	"context"
	"fmt"
	"log"

	"github.com/antithesishq/antithesis-sdk-go/assert"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
)

// createP2P creates a new libp2p host and an addr info.
func createP2P(ctx context.Context, name string) {
	h, err := libp2p.New()
	assert.Always(err == nil, "Successfully created a libp2p host", map[string]interface{}{"error": err})

	ai, err := peer.AddrInfoFromString(name)
	assert.Always(err == nil, "Successfully created an addr info", map[string]interface{}{"error": err})

	if err := h.Connect(ctx, *ai); err != nil {
		log.Fatal(err)
	}
}

func GetP2PMultiAddr(ctx context.Context, nodeConfig *NodeConfig) (string, error) {
	// Connect to Lotus node
	api, closer, err := ConnectToNode(ctx, *nodeConfig)
	if err != nil {
		return "", fmt.Errorf("failed to connect to Lotus node '%s': %v", nodeConfig.Name, err)
	}
	defer closer()

	// Get peer address information
	addrInfo, err := api.NetAddrsListen(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get peer info for node '%s': %v", nodeConfig.Name, err)
	}

	// Extract the first valid multiaddr
	for _, addrStr := range addrInfo.Addrs {
		multiAddr, err := multiaddr.NewMultiaddr(fmt.Sprintf("%s/p2p/%s", addrStr, addrInfo.ID))
		if err == nil {
			return multiAddr.String(), nil
		}
	}

	return "", fmt.Errorf("no valid P2P multiaddr found for node '%s'", nodeConfig.Name)
}
