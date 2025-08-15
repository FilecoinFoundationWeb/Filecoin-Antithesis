package mpoolfuzz

import (
	"context"
	"log"
	"math/rand"
	"sync"
	"time"

	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources"
	"github.com/antithesishq/antithesis-sdk-go/assert"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/ipfs/go-cid"
)

func SendStandardMutations(ctx context.Context, api api.FullNode, from, to address.Address, count int, r *rand.Rand) error {
	var acceptedCids []cid.Cid
	var mutationDescriptions []string

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
			mutationDescriptions = append(mutationDescriptions, description)
		}

		delay := time.Millisecond * time.Duration(50+r.Intn(200))
		time.Sleep(delay)
	}

	// Verify none of our mutated messages got mined
	if len(acceptedCids) > 0 {
		if err := checkStateWait(ctx, api, acceptedCids, mutationDescriptions); err != nil {
			log.Printf("[ERROR] Message verification failed: %v", err)
			return err
		}
	}

	return nil
}

// SendChainedTransactions implements chained transaction attacks
func SendChainedTransactions(ctx context.Context, api api.FullNode, from, to address.Address, count int, r *rand.Rand) error {
	var acceptedCids []cid.Cid
	var mutationDescriptions []string

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
	mutationDescriptions = append(mutationDescriptions, "initial valid message")

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
			msg.GasLimit = 1000000000000000000
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
			mutationDescriptions = append(mutationDescriptions, description)
		}

		delay := time.Millisecond * time.Duration(100+r.Intn(400))
		time.Sleep(delay)
	}

	// Verify none of our mutated messages got mined
	if len(acceptedCids) > 0 {
		if err := checkStateWait(ctx, api, acceptedCids, mutationDescriptions); err != nil {
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
	mutationDescriptions := make([]string, count)
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
			mutationDescriptions[i] = "Negative gas limit"
		} else if i%7 == 0 {
			msg.GasPremium = abi.NewTokenAmount(1e18)
			mutationDescriptions[i] = "Very high gas premium"
		} else {
			mutationDescriptions[i] = "No mutation"
		}

		messages[i] = msg
	}

	var acceptedCids []cid.Cid
	var acceptedDescriptions []string
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
				acceptedDescriptions = append(acceptedDescriptions, mutationDescriptions[res.index])
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
		if err := checkStateWait(ctx, api, acceptedCids, acceptedDescriptions); err != nil {
			log.Printf("[ERROR] Message verification failed: %v", err)
			return err
		}
	}

	return nil
}
func SendReorgAttack(ctx context.Context, api1, api2 api.FullNode, count int) error {
	log.Printf("[INFO] Starting reorg attack simulation")

	// Disconnect both nodes from other peers to create network partition
	err := resources.DisconnectFromOtherNodes(ctx, api1)
	if err != nil {
		log.Printf("[WARN] Failed to disconnect api1 from other nodes: %v", err)
	}

	err = resources.DisconnectFromOtherNodes(ctx, api2)
	if err != nil {
		log.Printf("[WARN] Failed to disconnect api2 from other nodes: %v", err)
	}

	// Wait a bit for disconnections to take effect
	time.Sleep(5 * time.Second)
	log.Printf("[INFO] Nodes disconnected, sending identical messages to both nodes")

	// Send transactions to both nodes
	var acceptedCids []cid.Cid
	var mutationDescriptions []string

	// Limit to fewer messages to avoid running out of funds
	maxMessages := 10
	if count > maxMessages {
		count = maxMessages
		log.Printf("[INFO] Limiting reorg attack to %d messages to avoid fund exhaustion", maxMessages)
	}

	// Get genesis wallet from api1
	genesisWallet, err := resources.GetGenesisWallet(ctx, api1)
	if err != nil {
		log.Printf("[ERROR] Failed to get genesis wallet: %v", err)
		return err
	}
	log.Printf("[INFO] Using genesis wallet %s for reorg attack", genesisWallet)

	// Export genesis wallet from api1 and import to api2
	walletExport, err := api1.WalletExport(ctx, genesisWallet)
	if err != nil {
		log.Printf("[ERROR] Failed to export genesis wallet from api1: %v", err)
		return err
	}
	importedAddr, err := api2.WalletImport(ctx, walletExport)
	if err != nil {
		log.Printf("[ERROR] Failed to import genesis wallet to api2: %v", err)
		return err
	}
	log.Printf("[INFO] Successfully imported genesis wallet to api2 as %s", importedAddr)

	// Check balance on api1
	balance, err := api1.WalletBalance(ctx, genesisWallet)
	if err != nil {
		log.Printf("[WARN] Failed to check wallet balance: %v", err)
	} else {
		log.Printf("[INFO] Wallet balance: %s FIL", types.FIL(balance))
		// Ensure we have enough balance for the attack
		requiredBalance := types.NewInt(uint64(count * 100000)) // Rough estimate
		if balance.LessThan(requiredBalance) {
			log.Printf("[WARN] Low wallet balance, reducing message count")
			count = int(balance.Int64() / 100000)
			if count < 1 {
				count = 1
			}
		}
	}

	for i := 0; i < count; i++ {
		// Create identical base message with minimal value and using default wallet
		msg := &types.Message{
			From:   genesisWallet,      // Use genesis wallet that we imported on both nodes
			To:     genesisWallet,      // Send to self to avoid needing external addresses
			Value:  types.NewInt(1000), // Minimal value (1000 attoFIL)
			Method: 0,
			Params: nil,
			// Omit Nonce, GasLimit, GasFeeCap, GasPremium - MpoolPushMessage will set these
		}
		description := "Reorg attack message"

		log.Printf("[REORG %d] Sending identical message to both nodes", i)

		// Send to Lotus-1
		smsg1, err1 := api1.MpoolPushMessage(ctx, msg, nil)
		if err1 == nil {
			log.Printf("[REORG %d] Accepted on Lotus-1: %s", i, smsg1.Cid())
			acceptedCids = append(acceptedCids, smsg1.Cid())
			mutationDescriptions = append(mutationDescriptions, description+" on Lotus-1")
		} else {
			log.Printf("[REORG %d] Rejected on Lotus-1: %v", i, err1)
		}

		// Send to Lotus-2
		smsg2, err2 := api2.MpoolPushMessage(ctx, msg, nil)
		if err2 == nil {
			log.Printf("[REORG %d] Accepted on Lotus-2: %s", i, smsg2.Cid())
			acceptedCids = append(acceptedCids, smsg2.Cid())
			mutationDescriptions = append(mutationDescriptions, description+" on Lotus-2")
		} else {
			log.Printf("[REORG %d] Rejected on Lotus-2: %v", i, err2)
		}

		// Small delay between messages
		time.Sleep(100 * time.Millisecond)
	}

	log.Printf("[INFO] Reconnecting nodes to allow consensus")

	// Try to get peer info from api2 and connect api1 to it
	peerInfo2, err := api2.NetAddrsListen(ctx)
	if err != nil {
		log.Printf("[WARN] Failed to get peer info from api2: %v", err)

		// Fallback: try using the robust ConnectToOtherNodes method
		config, configErr := resources.LoadConfig("/opt/antithesis/resources/config.json")
		if configErr != nil {
			log.Printf("[ERROR] Failed to load config for reconnection: %v", configErr)
			return configErr
		}

		// Filter to get only Lotus nodes
		lotusNodes := resources.FilterLotusNodes(config.Nodes)
		if len(lotusNodes) < 2 {
			log.Printf("[ERROR] Need at least 2 Lotus nodes for reconnection")
			return err
		}

		// Find the current node config (assuming api1 is the first Lotus node)
		currentNodeConfig := lotusNodes[0]

		// Reconnect to all other Lotus nodes using the existing robust method
		err = resources.ConnectToOtherNodes(ctx, api1, currentNodeConfig, lotusNodes)
		if err != nil {
			log.Printf("[WARN] Failed to reconnect using ConnectToOtherNodes: %v", err)
			// Continue anyway as this is not fatal for the test
		} else {
			log.Printf("[INFO] Successfully reconnected Lotus nodes using ConnectToOtherNodes")
		}
	} else {
		// Direct connection using peer info
		err = api1.NetConnect(ctx, peerInfo2)
		if err != nil {
			log.Printf("[WARN] Failed to directly reconnect api1 to api2: %v", err)
		} else {
			log.Printf("[INFO] Successfully reconnected api1 to api2 directly")
		}
	}

	log.Printf("[INFO] Waiting 60 seconds for consensus and finalization")
	time.Sleep(time.Second * 60)

	if len(acceptedCids) == 0 {
		log.Printf("[WARN] No messages were accepted during reorg attack")
		return nil
	}

	// Check state wait with non-fatal error handling
	err = checkStateWait(ctx, api1, acceptedCids, mutationDescriptions)
	if err != nil {
		log.Printf("[WARN] State wait check failed on Lotus-1: %v", err)
		// Continue with other checks
	}

	// Use the first accepted message for chain verification
	testCid := acceptedCids[0]
	foundOnChain1, err := IsMessageOnChain(ctx, api1, testCid)
	if err != nil {
		log.Printf("[WARN] Failed to check if message is on chain for Lotus-1: %v", err)
		foundOnChain1 = false
	}

	// Check if the message is on chain for Lotus-2
	foundOnChain2, err := IsMessageOnChain(ctx, api2, testCid)
	if err != nil {
		log.Printf("[WARN] Failed to check if message is on chain for Lotus-2: %v", err)
		foundOnChain2 = false
	}

	log.Printf("[REORG RESULT] Message %s found on chain - Lotus-1: %v, Lotus-2: %v",
		testCid, foundOnChain1, foundOnChain2)

	// The assertion: after reorg, the message should be found on at least one chain
	// but ideally not on both (as that would indicate a consensus failure)
	assert.Always(foundOnChain1 || foundOnChain2, "Message should be found on one chain after reorg", map[string]interface{}{
		"foundOnChain1": foundOnChain1,
		"foundOnChain2": foundOnChain2,
		"messageCid":    testCid.String(),
		"requirement":   "After reorg, message should be found on at least one chain",
	})

	if foundOnChain1 && foundOnChain2 {
		log.Printf("[WARN] Message found on both chains - potential consensus issue")
	} else if foundOnChain1 || foundOnChain2 {
		log.Printf("[SUCCESS] Reorg attack completed successfully - message found on exactly one chain")
	} else {
		log.Printf("[ERROR] Message not found on either chain - potential issue with test")
	}

	return nil
}
