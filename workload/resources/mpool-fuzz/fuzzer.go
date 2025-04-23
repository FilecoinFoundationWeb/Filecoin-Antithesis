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

	attackStrategy := r.Intn(5)

	var err error
	switch attackStrategy {
	case 0:
		log.Printf("Strategy: Standard mutation attacks")
		err = SendStandardMutations(ctx, api, from, to, config.Count, r)
	case 1:
		log.Printf("Strategy: Chained transaction attacks")
		err = SendChainedTransactions(ctx, api, from, to, config.Count, r)
	case 2:
		log.Printf("Strategy: Burst with concurrent requests")
		err = SendConcurrentBurst(ctx, api, from, to, config.Count, r, config.Concurrenct)
	case 3:
		log.Printf("Strategy: Mixed subtle attacks")
		err = SendSubtleAttacks(ctx, api, from, to, config.Count, r)
	case 4:
		log.Printf("Strategy: Protocol edge cases")
		err = SendEdgeCases(ctx, api, from, to, config.Count, r)
	default:
		log.Printf("Strategy: Fallback to standard mutations")
		err = SendStandardMutations(ctx, api, from, to, config.Count, r)
	}

	// Optionally replay transactions
	if config.EnableReplay && len(GetStoredSignedMessages()) > 0 {
		log.Printf("Replaying stored messages")
		ReplayStoredMessages(ctx, api)
	}

	return err
}

func SendSubtleAttacks(ctx context.Context, api api.FullNode, from, to address.Address, count int, r *rand.Rand) error {
	nonce, err := api.MpoolGetNonce(ctx, from)
	if err != nil {
		log.Printf("[WARN] Could not get nonce for %s: %v, using 0", from, err)
		nonce = 0
	}

	for i := 0; i < count; i++ {
		msg := CreateBaseMessage(from, to, nonce)

		mutationType := GetRandomMutation("subtle", r)
		description := Apply(msg, mutationType, r)

		log.Printf("[Subtle %d] %s", i, description)
		smsg, err := api.MpoolPushMessage(ctx, msg, nil)

		if err != nil {
			log.Printf("[rejected] Subtle tx %d: %v", i, err)
		} else {
			log.Printf("[ACCEPTED] Subtle tx %d was accepted: %s", i, smsg.Cid())
			StoreSignedMessage(smsg)
			nonce++
		}

		time.Sleep(time.Millisecond * time.Duration(100+r.Intn(200)))
	}

	return nil
}

// SendEdgeCases implements protocol edge case attacks
func SendEdgeCases(ctx context.Context, api api.FullNode, from, to address.Address, count int, r *rand.Rand) error {
	nonce, err := api.MpoolGetNonce(ctx, from)
	if err != nil {
		log.Printf("[WARN] Could not get nonce for %s: %v, using 0", from, err)
		nonce = 0
	}

	for i := 0; i < count; i++ {
		msg := CreateBaseMessage(from, to, nonce+uint64(i))

		mutationType := GetRandomMutation("edge", r)
		description := Apply(msg, mutationType, r)

		log.Printf("[Edge %d] Testing: %s", i, description)
		smsg, err := api.MpoolPushMessage(ctx, msg, nil)

		if err != nil {
			log.Printf("[rejected] Edge tx %d: %v", i, err)
		} else {
			log.Printf("[ACCEPTED] Edge tx %d was accepted: %s", i, smsg.Cid())
			StoreSignedMessage(smsg)
		}

		time.Sleep(time.Millisecond * 250)
	}

	return nil
}
