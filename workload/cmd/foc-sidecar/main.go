package main

import (
	"context"
	"log"
	"time"

	"workload/internal/chain"
	"workload/internal/foc"

	"github.com/antithesishq/antithesis-sdk-go/lifecycle"
	"github.com/filecoin-project/lotus/api"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.Println("[foc-sidecar] starting")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Load FOC config
	cfg := foc.ParseEnvironment()
	if cfg == nil {
		log.Fatal("[foc-sidecar] FATAL: FOC environment not found — this binary should only run in FOC profile")
	}

	// Connect to lotus0
	nodeCfg := chain.NodeConfig{
		Names: []string{"lotus0"},
		Port:  "1234",
	}
	nodes, nodeKeys, err := chain.ConnectNodes(ctx, nodeCfg)
	if err != nil {
		log.Fatalf("[foc-sidecar] FATAL: cannot connect to lotus: %v", err)
	}
	node := nodes[nodeKeys[0]]

	state := NewSidecarState()

	// Signal setup complete after first successful poll
	setupDone := false

	var lastPolledBlock uint64
	var pollCount uint64

	log.Println("[foc-sidecar] entering polling loop")

	for {
		// Get latest tipset height
		head, err := node.ChainHead(ctx)
		if err != nil {
			log.Printf("[foc-sidecar] ChainHead error: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}

		// Use a small finality window (30 epochs) to avoid reorg noise
		currentHeight := uint64(head.Height())
		finalizedHeight := currentHeight
		if finalizedHeight > 30 {
			finalizedHeight -= 30
		}

		if finalizedHeight <= lastPolledBlock {
			time.Sleep(4 * time.Second)
			continue
		}

		fromBlock := lastPolledBlock + 1
		if lastPolledBlock == 0 {
			// On first poll, start from a recent window rather than genesis
			if finalizedHeight > 100 {
				fromBlock = finalizedHeight - 100
			} else {
				fromBlock = 1
			}
		}

		// Poll for events and update state
		pollEvents(ctx, node, cfg, state, fromBlock, finalizedHeight)

		// Run assertions
		checkRailToDataset(ctx, node, cfg, state)
		checkFilecoinPaySolvency(ctx, node, cfg, state)
		checkProviderIDConsistency(ctx, node, cfg, state)
		checkProofSetLiveness(ctx, node, cfg, state)
		checkDeletedDataSetNotLive(ctx, node, cfg, state)
		checkActivePieceCount(ctx, node, cfg, state)
		checkProvingAdvancement(ctx, node, cfg, state)
		checkPieceAccountingConsistency(ctx, node, cfg, state)
		checkRateConsistency(ctx, node, cfg, state)


		lastPolledBlock = finalizedHeight
		pollCount++

		// Periodic status log every 10 polls
		if pollCount%10 == 0 {
			ds := state.GetDatasets()
			payers := state.GetTrackedPayers()
			log.Printf("[foc-sidecar] poll #%d: scanned blocks %d-%d, datasets=%d payers=%d",
				pollCount, fromBlock, finalizedHeight, len(ds), len(payers))
		}

		if !setupDone {
			lifecycle.SetupComplete(map[string]any{
				"component": "foc-sidecar",
			})
			setupDone = true
			log.Println("[foc-sidecar] setup complete, polling active")
		}

		time.Sleep(4 * time.Second)
	}
}

// pollEvents fetches logs for all tracked event types and updates state.
func pollEvents(ctx context.Context, node api.FullNode, cfg *foc.Config, state *SidecarState, from, to uint64) {
	// DataSetCreated events from FWSS
	if cfg.FWSSAddr != nil {
		logs, err := fetchAndParseLogs(ctx, node, cfg.FWSSAddr, TopicDataSetCreated, from, to)
		if err != nil {
			log.Printf("[foc-sidecar] fetchLogs(DataSetCreated) error: %v", err)
		} else {
			events := parseDataSetCreatedLogs(logs)
			for _, ev := range events {
				log.Printf("[foc-sidecar] DataSetCreated: dsId=%s providerId=%s pdpRailId=%s payer=%x sp=%x payee=%x",
					ev.DataSetID, ev.ProviderID, ev.PDPRailID, ev.Payer, ev.ServiceProvider, ev.Payee)
				state.AddDataset(ev)
			}
		}
	}

	// DataSetDeleted events from PDPVerifier
	if cfg.PDPAddr != nil {
		logs, err := fetchAndParseLogs(ctx, node, cfg.PDPAddr, TopicDataSetDeleted, from, to)
		if err != nil {
			log.Printf("[foc-sidecar] fetchLogs(DataSetDeleted) error: %v", err)
		} else {
			events := parseDataSetDeletedLogs(logs)
			for _, ev := range events {
				log.Printf("[foc-sidecar] DataSetDeleted: dsId=%s leafCount=%s",
					ev.DataSetID, ev.DeletedLeafCount)
				state.MarkDatasetDeleted(ev.DataSetID.Uint64())
			}
		}
	}

	// RailCreated events from FilecoinPay
	if cfg.FilPayAddr != nil {
		logs, err := fetchAndParseLogs(ctx, node, cfg.FilPayAddr, TopicRailCreated, from, to)
		if err != nil {
			log.Printf("[foc-sidecar] fetchLogs(RailCreated) error: %v", err)
		} else {
			events := parseRailCreatedLogs(logs)
			for _, ev := range events {
				log.Printf("[foc-sidecar] RailCreated: railId=%s token=%x from=%x to=%x",
					ev.RailID, ev.Token, ev.From, ev.To)
				state.AddRail(ev)
			}
		}
	}
}
