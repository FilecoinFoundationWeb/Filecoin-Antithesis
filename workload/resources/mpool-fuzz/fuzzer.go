// workload/resources/mpool-fuzz/fuzzer.go
package mpoolfuzz

import (
	"context"
	"log"
	"math/rand"
	"time"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/lotus/api"
)

// RunFuzzer is the main entry point for mempool fuzzing
func RunFuzzer(ctx context.Context, api api.FullNode, from, to address.Address, config *Config) error {
	log.Printf("Starting mempool fuzzing with %d transactions", config.Count)

	seed := config.Seed
	if seed == 0 {
		seed = time.Now().UnixNano()
	}
	r := rand.New(rand.NewSource(seed))

	var err error
	switch config.AttackType {
	case SimpleAttack:
		log.Printf("Strategy: Simple mutation attacks")
		err = SendStandardMutations(ctx, api, from, to, config.Count, r)
	case ChainedAttack:
		log.Printf("Strategy: Chained transaction attacks")
		err = SendChainedTransactions(ctx, api, from, to, config.Count, r)
	default:
		log.Printf("Strategy: Fallback to simple mutations")
		err = SendStandardMutations(ctx, api, from, to, config.Count, r)
	}

	return err
}
