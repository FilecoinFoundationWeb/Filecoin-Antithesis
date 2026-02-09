package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/wallet/key"
	_ "github.com/filecoin-project/lotus/lib/sigs/secp"
	"github.com/urfave/cli/v2"
)

type GenesisAccount struct {
	Type    string `json:"Type"`
	Balance string `json:"Balance"`
	Meta    struct {
		Owner string `json:"Owner"`
	} `json:"Meta"`
}

type KeystoreEntry struct {
	Address    string `json:"Address"`
	PrivateKey string `json:"PrivateKey"` // Hex encoded
}

func main() {
	app := &cli.App{
		Name:  "genesis-prep",
		Usage: "Generate deterministic Filecoin wallets for genesis injection",
		Flags: []cli.Flag{
			&cli.IntFlag{
				Name:    "count",
				Aliases: []string{"n"},
				Value:   100,
				Usage:   "Number of wallets to generate",
			},
			&cli.StringFlag{
				Name:    "out",
				Aliases: []string{"o"},
				Value:   "/shared",
				Usage:   "Output directory for JSON files",
			},
			&cli.StringFlag{
				Name:  "balance",
				Value: "10000000000000000000000", // 10,000 FIL
				Usage: "Initial balance in attoFIL",
			},
		},
		Action: func(c *cli.Context) error {
			return generate(c.Int("count"), c.String("out"), c.String("balance"))
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}

func generate(count int, outDir string, balance string) error {
	log.Printf("Generating %d wallets...", count)

	var genesisAccs []GenesisAccount
	var keystore []KeystoreEntry

	for i := 0; i < count; i++ {
		k, err := key.GenerateKey(types.KTSecp256k1)
		if err != nil {
			return fmt.Errorf("failed to generate key: %w", err)
		}
		genesisAccs = append(genesisAccs, GenesisAccount{
			Type:    "account",
			Balance: balance,
			Meta: struct {
				Owner string `json:"Owner"`
			}{Owner: k.Address.String()},
		})

		keystore = append(keystore, KeystoreEntry{
			Address:    k.Address.String(),
			PrivateKey: hex.EncodeToString(k.KeyInfo.PrivateKey),
		})
	}

	// 4. Write Files
	if err := writeJson(fmt.Sprintf("%s/genesis_allocs.json", outDir), genesisAccs); err != nil {
		return err
	}
	if err := writeJson(fmt.Sprintf("%s/stress_keystore.json", outDir), keystore); err != nil {
		return err
	}

	log.Printf("Success! Wrote keys to %s", outDir)
	return nil
}

func writeJson(path string, data interface{}) error {
	b, _ := json.MarshalIndent(data, "", "  ")
	return os.WriteFile(path, b, 0644)
}
