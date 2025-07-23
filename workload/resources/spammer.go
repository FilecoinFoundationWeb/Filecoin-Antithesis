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
	if len(wallets) < 2 || len(wallets[0]) < 1 || len(wallets[1]) < 1 {
		fmt.Printf("not enough wallets to spam transactions; need wallets on both nodes")
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

		from := wallets[fromNodeIndex][rand.Intn(len(wallets[fromNodeIndex]))]
		to := wallets[toNodeIndex][rand.Intn(len(wallets[toNodeIndex]))]

		if fromNodeIndex == toNodeIndex && from == to {
			continue
		}

		amount := abi.NewTokenAmount(int64(rand.Intn(1) + 1)) // Random amount up to 50 FIL

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
