package main

// func main() {
// 	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)

// 	config, err := resources.LoadConfig("/opt/antithesis/resources/config.json")
// 	if err != nil {
// 		log.Fatal("failed to load config", err)
// 	}

// 	if len(config.Nodes) < 2 {
// 		log.Fatal("need at least two nodes")
// 	}

// 	node1 := config.Nodes[0]
// 	api1, closer1, err := resources.ConnectToNode(ctx, node1)
// 	if err != nil {
// 		log.Fatal("failed to connect to node", err)
// 	}
// 	defer closer1()
// 	manifest, _ := api1.F3GetManifest(ctx)
// 	networkName := manifest.NetworkName
// 	h, _ := libp2p.New()
// 	defer cancel()

// 	targetAddr := os.Getenv("LOTUS_TARGET")
// 	if targetAddr == "" {
// 		log.Fatal("LOTUS_TARGET environment variable not set")
// 	}

// 	maddr, _ := ma.NewMultiaddr(targetAddr)
// 	pi, _ := peer.AddrInfoFromP2pAddr(maddr)
// 	if err := h.Connect(ctx, *pi); err != nil {
// 		log.Fatal("dial:", err)
// 	}
// 	client := certexchange.Client{
// 		Host:           h,
// 		NetworkName:    gpbft.NetworkName(networkName),
// 		RequestTimeout: 30 * time.Second,
// 	}

// 	req := certexchange.Request{
// 		FirstInstance:     0,
// 		Limit:             1_000_000, // unbounded
// 		IncludePowerTable: false,
// 	}

// 	_, ch, err := client.Request(ctx, pi.ID, &req)
// 	if err != nil {
// 		log.Fatal("request:", err)
// 	}

// 	var received uint64
// 	for range ch {
// 		received++
// 		if received%10_000 == 0 {
// 			log.Println("received", received)
// 		}
// 	}
// }
