package resources

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"time"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
)

// SpamTransactionsBetweenNodes spams transactions between wallets on multiple connected nodes.
func SpamTransactions(ctx context.Context, apis []api.FullNode, wallets [][]address.Address, numTransactions int) error {
	if len(wallets) < 2 || len(wallets[0]) < 1 || len(wallets[1]) < 1 {
		fmt.Printf("not enough wallets to spam transactions; need wallets on both nodes")
		return nil
	}

	// Randomized transaction options
	transactionOptions := []int{10, 25, 50, 80, 100}
	nTransactions := transactionOptions[rand.Intn(len(transactionOptions))]
	cooldownOptions := []float64{0.25, 0.5, 0.75, 1.0}
	cooldown := time.Duration(cooldownOptions[rand.Intn(len(cooldownOptions))]*1000) * time.Millisecond

	log.Printf("Starting spammer with %d transactions and %v cooldown\n", nTransactions, cooldown)

	for i := 0; i < numTransactions; i++ {
		// Randomly select wallets from connected nodes
		fromNodeIndex := rand.Intn(len(apis))
		toNodeIndex := rand.Intn(len(apis))

		from := wallets[fromNodeIndex][rand.Intn(len(wallets[fromNodeIndex]))]
		to := wallets[toNodeIndex][rand.Intn(len(wallets[toNodeIndex]))]

		// Avoid self-transactions
		if fromNodeIndex == toNodeIndex && from == to {
			continue
		}

		// Random transaction amount
		amount := abi.NewTokenAmount(int64(rand.Intn(50) + 1)) // Random amount up to 50 FIL

		// Check the sender's wallet balance
		balance, err := apis[fromNodeIndex].WalletBalance(ctx, from)
		if err != nil {
			log.Printf("Failed to fetch balance for wallet %s: %v", from, err)
			continue
		}

		if balance.LessThan(amount) {
			log.Printf("Skipping transaction #%d: Insufficient balance in wallet %s. Balance: %s, Required: %s",
				i+1, from, balance.String(), amount.String())
			continue
		}

		log.Printf("Transaction #%d: Sending %s from %s on Node%d to %s on Node%d", i+1, amount.String(), from, fromNodeIndex+1, to, toNodeIndex+1)

		// Execute the transaction
		err = SendFunds(ctx, apis[fromNodeIndex], from, to, amount)
		if err != nil {
			log.Printf("Failed transaction #%d: %v", i+1, err)
		} else {
			log.Printf("Transaction #%d succeeded", i+1)
		}

		time.Sleep(cooldown)
	}

	log.Println("Transaction spamming completed.")
	return nil
}
