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

// SpamTransactions generates and sends random transactions between wallets across different nodes
// It randomly selects transaction amounts, cooldown periods, and source/destination wallets
// to simulate various transaction patterns and test network behavior under load
func SpamTransactions(ctx context.Context, apis []api.FullNode, wallets [][]address.Address, numTransactions int) error {
	if len(apis) == 0 || len(wallets) == 0 {
		fmt.Printf("No APIs or wallets available for spamming transactions")
		return nil
	}

	// Check if we have at least one node with wallets
	hasWallets := false
	for _, nodeWallets := range wallets {
		if len(nodeWallets) > 0 {
			hasWallets = true
			break
		}
	}

	if !hasWallets {
		fmt.Printf("No wallets available on any node for spamming transactions")
		return nil
	}

	transactionOptions := []int{10, 25, 50, 80, 100}
	nTransactions := transactionOptions[rand.Intn(len(transactionOptions))]
	cooldownOptions := []float64{0.25, 0.5, 0.75, 1.0}
	cooldown := time.Duration(cooldownOptions[rand.Intn(len(cooldownOptions))]*1000) * time.Millisecond

	log.Printf("Starting spammer with %d transactions and %v cooldown\n", nTransactions, cooldown)

	for i := 0; i < numTransactions; i++ {
		fromNodeIndex := rand.Intn(len(apis))
		toNodeIndex := rand.Intn(len(apis))

		// Check if wallets exist for both nodes
		if len(wallets[fromNodeIndex]) == 0 || len(wallets[toNodeIndex]) == 0 {
			log.Printf("Skipping transaction #%d: No wallets available on one or both nodes", i+1)
			continue
		}

		from := wallets[fromNodeIndex][rand.Intn(len(wallets[fromNodeIndex]))]
		to := wallets[toNodeIndex][rand.Intn(len(wallets[toNodeIndex]))]

		if fromNodeIndex == toNodeIndex && from == to {
			continue
		}

		amount := abi.NewTokenAmount(int64(rand.Intn(10) + 1)) // Random amount 1-10 FIL

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
