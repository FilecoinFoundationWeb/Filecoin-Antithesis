package main

import (
	"log"
	"os"
	"strconv"

	"github.com/antithesishq/antithesis-sdk-go/random"
)

// ---------------------------------------------------------------------------
// Configuration helpers
// ---------------------------------------------------------------------------

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		log.Printf("[protocol-fuzzer] invalid int for %s=%q, using default %d", key, v, fallback)
		return fallback
	}
	return n
}

// ---------------------------------------------------------------------------
// Randomness helpers (Antithesis SDK - deterministic)
// ---------------------------------------------------------------------------

func rngIntn(n int) int {
	if n <= 0 {
		return 0
	}
	return int(random.GetRandom() % uint64(n))
}

func rngChoice[T any](items []T) T {
	return random.RandomChoice(items)
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(random.GetRandom() % 256)
	}
	return b
}

// ---------------------------------------------------------------------------
// Debug logging
// ---------------------------------------------------------------------------

var debugLogging = os.Getenv("FUZZER_DEBUG") == "1"

func debugLog(format string, args ...any) {
	if debugLogging {
		log.Printf(format, args...)
	}
}
