package mpoolfuzz

import (
	"context"
	"math/rand"
	"time"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/lotus/api"
)

func FuzzMempool(ctx context.Context, api api.FullNode, from, to address.Address, customConfig *Config) error {
	config := DefaultConfig()
	if customConfig != nil {
		config = customConfig
	}

	return RunFuzzer(ctx, api, from, to, config)
}

func FuzzMempoolWithStrategy(ctx context.Context, api api.FullNode, from, to address.Address, strategy string, count int) error {
	config := DefaultConfig()
	config.Count = count

	// Set up random generator
	seed := config.Seed
	if seed == 0 {
		seed = time.Now().UnixNano()
	}
	r := rand.New(rand.NewSource(seed))

	var err error
	switch strategy {
	case "standard":
		err = SendStandardMutations(ctx, api, from, to, count, r)
	case "chained":
		err = SendChainedTransactions(ctx, api, from, to, count, r)
	case "burst":
		err = SendConcurrentBurst(ctx, api, from, to, count, r, config.Concurrenct)
	case "subtle":
		err = SendSubtleAttacks(ctx, api, from, to, count, r)
	case "edge":
		err = SendEdgeCases(ctx, api, from, to, count, r)
	default:
		// Default to standard mutations
		err = SendStandardMutations(ctx, api, from, to, count, r)
	}

	return err
}

// GetAvailableStrategies returns the list of available strategies
func GetAvailableStrategies() []string {
	return []string{
		"standard",
		"chained",
		"burst",
		"subtle",
		"edge",
	}
}
