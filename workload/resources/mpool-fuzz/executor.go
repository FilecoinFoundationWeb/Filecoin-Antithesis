package mpoolfuzz

import (
	"context"
	"log"
	"math/rand"
	"sync"
	"time"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/ipfs/go-cid"
)

func SendStandardMutations(ctx context.Context, api api.FullNode, from, to address.Address, count int, r *rand.Rand) error {
	var acceptedCids []cid.Cid

	for i := 0; i < count; i++ {
		// Create base message
		startingNonce, err := api.MpoolGetNonce(ctx, from)
		if err != nil {
			log.Printf("[WARN] Could not get nonce for %s: %v, using 0", from, err)
			startingNonce = 0
		}
		msg := CreateBaseMessage(from, to, startingNonce)

		// Apply mutation
		mutationType := GetRandomMutation("standard", r)
		description := Apply(msg, mutationType, r)

		// Send message
		log.Printf("[Test %d] %s", i, description)
		smsg, err := api.MpoolPushMessage(ctx, msg, nil)

		if err != nil {
			log.Printf("[rejected] tx %d: %v", i, err)
		} else {
			msgCid := smsg.Cid()
			log.Printf("[ACCEPTED] tx %d was accepted: %s", i, msgCid)
			acceptedCids = append(acceptedCids, msgCid)
		}

		delay := time.Millisecond * time.Duration(50+r.Intn(200))
		time.Sleep(delay)
	}

	// Verify none of our mutated messages got mined
	if len(acceptedCids) > 0 {
		if err := checkStateWait(ctx, api, acceptedCids); err != nil {
			log.Printf("[ERROR] Message verification failed: %v", err)
			return err
		}
	}

	return nil
}

// SendChainedTransactions implements chained transaction attacks
func SendChainedTransactions(ctx context.Context, api api.FullNode, from, to address.Address, count int, r *rand.Rand) error {
	var acceptedCids []cid.Cid

	nonce, err := api.MpoolGetNonce(ctx, from)
	if err != nil {
		log.Printf("[WARN] Could not get nonce for %s: %v, using 0", from, err)
		nonce = 0
	}

	// First send a valid message
	validMsg := CreateBaseMessage(from, from, nonce)

	validSigned, err := api.MpoolPushMessage(ctx, validMsg, nil)
	if err != nil {
		log.Printf("[ERROR] Failed to send initial valid message: %v", err)
		return SendStandardMutations(ctx, api, from, to, count, r)
	}

	log.Printf("[INFO] Sent initial valid message with CID: %s", validSigned.Cid())
	acceptedCids = append(acceptedCids, validSigned.Cid())

	time.Sleep(time.Millisecond * 500)

	// Send chain of related messages
	for i := 1; i < count; i++ {
		nonce, err = api.MpoolGetNonce(ctx, from)
		if err != nil {
			log.Printf("[WARN] Could not get nonce for %s: %v, using 0", from, err)
			nonce = 0
		}

		msg := CreateBaseMessage(from, from, nonce)

		// Apply chain-specific mutation
		var description string
		switch i % 4 {
		case 0:
			msg.Params = []byte{0x01}
			description = "Normal looking with random params"
		case 1:
			msg.GasLimit = 10000000000
			description = "Extremely high gas limit"
		case 2:
			msg.Method = 99
			description = "Invalid method number"
		case 3:
			randomAddr, err := address.NewIDAddress(uint64(r.Intn(100) + 100))
			if err != nil {
				randomAddr = from
			}
			msg.To = randomAddr
			msg.Value = abi.NewTokenAmount(1)
			description = "Transfer to random address " + randomAddr.String()
		}

		log.Printf("[Chain %d] %s", i, description)
		smsg, err := api.MpoolPushMessage(ctx, msg, nil)

		if err != nil {
			log.Printf("[rejected] Chain tx %d: %v", i, err)
		} else {
			msgCid := smsg.Cid()
			log.Printf("[ACCEPTED] Chain tx %d was accepted: %s", i, msgCid)
			acceptedCids = append(acceptedCids, msgCid)
		}

		delay := time.Millisecond * time.Duration(100+r.Intn(400))
		time.Sleep(delay)
	}

	// Verify none of our mutated messages got mined
	if len(acceptedCids) > 0 {
		if err := checkStateWait(ctx, api, acceptedCids); err != nil {
			log.Printf("[ERROR] Message verification failed: %v", err)
			return err
		}
	}

	return nil
}

// SendConcurrentBurst implements concurrent burst attacks
func SendConcurrentBurst(ctx context.Context, api api.FullNode, from, to address.Address, count int, r *rand.Rand, concurrency int) error {
	// Generate messages
	messages := make([]*types.Message, count)
	for i := 0; i < count; i++ {
		nonce, err := api.MpoolGetNonce(ctx, from)
		if err != nil {
			log.Printf("[WARN] Could not get nonce for %s: %v, using 0", from, err)
			nonce = 0
		}
		msg := CreateBaseMessage(from, to, nonce)

		// Apply some mutations
		if i%3 == 0 {
			msg.GasLimit = -1
		} else if i%7 == 0 {
			msg.GasPremium = abi.NewTokenAmount(1e18)
		}

		messages[i] = msg
	}

	var acceptedCids []cid.Cid
	var cidsMutex sync.Mutex

	// Set up result channel
	results := make(chan struct {
		index int
		err   error
		cid   cid.Cid
	}, count)

	log.Printf("[BURST] Sending %d concurrent messages", count)

	// Start receiver goroutine
	go func() {
		accepted := 0
		rejected := 0

		for res := range results {
			if res.err != nil {
				rejected++
				log.Printf("[BURST %d/%d] Rejected: %v", rejected, count, res.err)
			} else {
				accepted++
				log.Printf("[BURST %d/%d] Accepted: %s", accepted, count, res.cid)
				cidsMutex.Lock()
				acceptedCids = append(acceptedCids, res.cid)
				cidsMutex.Unlock()
			}
		}

		log.Printf("[BURST COMPLETE] %d accepted, %d rejected", accepted, rejected)
	}()

	// Limit concurrency
	if concurrency <= 0 || concurrency > count {
		concurrency = count
	}

	// Create worker pool
	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)

	// Send messages
	for i := 0; i < count; i++ {
		wg.Add(1)
		sem <- struct{}{} // Acquire semaphore

		go func(idx int) {
			defer wg.Done()
			defer func() { <-sem }() // Release semaphore

			time.Sleep(time.Millisecond * time.Duration(r.Intn(50)))
			smsg, err := api.MpoolPushMessage(ctx, messages[idx], nil)

			var msgCid cid.Cid
			if err == nil && smsg != nil {
				msgCid = smsg.Cid()
			}

			results <- struct {
				index int
				err   error
				cid   cid.Cid
			}{idx, err, msgCid}
		}(i)
	}

	wg.Wait()
	close(results)

	// Verify none of our mutated messages got mined
	if len(acceptedCids) > 0 {
		if err := checkStateWait(ctx, api, acceptedCids); err != nil {
			log.Printf("[ERROR] Message verification failed: %v", err)
			return err
		}
	}

	return nil
}
