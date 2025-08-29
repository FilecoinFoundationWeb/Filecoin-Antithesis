package resources

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/common"
)

const (
	defaultPassword = "password123"
	keystoreFile    = "pdp-keystore.json"
)

// CreateEthKeystore creates an Ethereum keystore and returns the address and keystore path
func CreateEthKeystore(keystoreDir string) (common.Address, string, error) {
	// Create keystore directory if it doesn't exist
	if err := os.MkdirAll(keystoreDir, 0755); err != nil {
		return common.Address{}, "", fmt.Errorf("failed to create keystore directory: %w", err)
	}

	// Create keystore with fixed filename
	keystorePath := filepath.Join(keystoreDir, keystoreFile)
	ks := keystore.NewKeyStore(keystoreDir, keystore.StandardScryptN, keystore.StandardScryptP)

	// Delete existing keystore if it exists
	if _, err := os.Stat(keystorePath); err == nil {
		if err := os.Remove(keystorePath); err != nil {
			return common.Address{}, "", fmt.Errorf("failed to remove existing keystore: %w", err)
		}
	}

	account, err := ks.NewAccount(defaultPassword)
	if err != nil {
		return common.Address{}, "", fmt.Errorf("failed to create account: %w", err)
	}

	// Rename the generated file to our fixed name
	if err := os.Rename(account.URL.Path, keystorePath); err != nil {
		return common.Address{}, "", fmt.Errorf("failed to rename keystore file: %w", err)
	}

	log.Printf("Created Ethereum account: %s", account.Address.Hex())
	log.Printf("Keystore saved at: %s", keystorePath)

	return account.Address, keystorePath, nil
}

func FundEthKeystore(keystoreDir string, amount string) {
	ks := keystore.NewKeyStore(keystoreDir, keystore.StandardScryptN, keystore.StandardScryptP)
	account, err := ks.NewAccount("password")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(account.Address.Hex())

}
