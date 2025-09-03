package resources

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

const (
	defaultPassword = "password123"
	keystoreFile    = "keystore.json"
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

	// Add private key to keystore.json for easy extraction with jq
	if err := addPrivateKeyToKeystore(keystorePath, defaultPassword); err != nil {
		log.Printf("[WARN] Failed to add private key to keystore: %v", err)
	}

	log.Printf("Created Ethereum account: %s", account.Address.Hex())
	log.Printf("Keystore saved at: %s", keystorePath)

	return account.Address, keystorePath, nil
}

// addPrivateKeyToKeystore adds the private key to the keystore.json file for easy extraction with jq
func addPrivateKeyToKeystore(keystorePath, password string) error {
	// Read the keystore file
	keystoreData, err := os.ReadFile(keystorePath)
	if err != nil {
		return fmt.Errorf("failed to read keystore file: %w", err)
	}

	// Decrypt the keystore to get the private key
	key, err := keystore.DecryptKey(keystoreData, password)
	if err != nil {
		return fmt.Errorf("failed to decrypt keystore: %w", err)
	}

	// Convert private key to hex string
	privateKeyHex := fmt.Sprintf("%x", crypto.FromECDSA(key.PrivateKey))

	// Parse the existing keystore JSON
	var keystoreJSON map[string]interface{}
	if err := json.Unmarshal(keystoreData, &keystoreJSON); err != nil {
		return fmt.Errorf("failed to parse keystore JSON: %w", err)
	}

	// Add private key to the JSON
	keystoreJSON["privateKey"] = privateKeyHex

	// Write the updated keystore back to file
	updatedData, err := json.MarshalIndent(keystoreJSON, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal updated keystore: %w", err)
	}

	if err := os.WriteFile(keystorePath, updatedData, 0600); err != nil {
		return fmt.Errorf("failed to write updated keystore: %w", err)
	}

	log.Printf("Added private key to keystore.json")
	return nil
}

func FundEthKeystore(keystoreDir string, amount string) {
	ks := keystore.NewKeyStore(keystoreDir, keystore.StandardScryptN, keystore.StandardScryptP)
	account, err := ks.NewAccount("password")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(account.Address.Hex())
}
