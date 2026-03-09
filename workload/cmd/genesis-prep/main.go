package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"

	"golang.org/x/crypto/hkdf"

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
			&cli.StringFlag{
				Name:  "seed",
				Value: "antithesis-stress-genesis-v1",
				Usage: "Master seed for deterministic key derivation",
			},
		},
		Action: func(c *cli.Context) error {
			return generate(c.Int("count"), c.String("out"), c.String("balance"), c.String("seed"))
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}

// derivePrivKey derives a secp256k1 private key deterministically from a master
// seed and wallet index using HKDF-SHA256. The same seed+index always produces
// the same 32-byte key, so wallets are stable across container restarts.
func derivePrivKey(masterSeed string, index int) ([]byte, error) {
	info := fmt.Sprintf("stress-wallet-%d", index)
	r := hkdf.New(sha256.New, []byte(masterSeed), nil, []byte(info))
	pk := make([]byte, 32)
	if _, err := io.ReadFull(r, pk); err != nil {
		return nil, fmt.Errorf("hkdf read failed: %w", err)
	}
	return pk, nil
}

func generate(count int, outDir string, balance string, seed string) error {
	log.Printf("Generating %d wallets (deterministic, seed=%q)...", count, seed)

	var genesisAccs []GenesisAccount
	var keystore []KeystoreEntry

	for i := 0; i < count; i++ {
		pk, err := derivePrivKey(seed, i)
		if err != nil {
			return fmt.Errorf("failed to derive key %d: %w", i, err)
		}
		k, err := key.NewKey(types.KeyInfo{Type: types.KTSecp256k1, PrivateKey: pk})
		if err != nil {
			return fmt.Errorf("failed to build key %d: %w", i, err)
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
