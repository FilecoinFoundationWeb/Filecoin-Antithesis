package mpoolfuzz

import (
	"sync"

	"github.com/filecoin-project/lotus/chain/types"
)

type Config struct {
	Count          int
	Seed           int64
	EnableReplay   bool
	Concurrenct    int
	EnabledAttacks []string
}

func DefaultConfig() *Config {
	return &Config{
		Count:          100,
		Seed:           1,
		EnableReplay:   true,
		Concurrenct:    5,
		EnabledAttacks: []string{},
	}
}

// Global store for signed messages to enable replay attacks
var (
	storedSignedMessages []*types.SignedMessage
	storedMutex          sync.Mutex
)

// StoreSignedMessage adds a message to the global store
func StoreSignedMessage(msg *types.SignedMessage) {
	storedMutex.Lock()
	defer storedMutex.Unlock()
	storedSignedMessages = append(storedSignedMessages, msg)
}

// GetStoredSignedMessages returns a copy of all stored messages
func GetStoredSignedMessages() []*types.SignedMessage {
	storedMutex.Lock()
	defer storedMutex.Unlock()

	result := make([]*types.SignedMessage, len(storedSignedMessages))
	copy(result, storedSignedMessages)
	return result
}
