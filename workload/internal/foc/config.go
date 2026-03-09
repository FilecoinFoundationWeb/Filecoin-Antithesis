package foc

import (
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"strings"

	secp256k1 "github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/lotus/chain/types/ethtypes"
)

// Config holds all addresses and keys parsed from /shared/environment.env.
// Shared between stress-engine and foc-sidecar.
type Config struct {
	// Contract addresses (20-byte slices)
	USDFCAddr    []byte
	FilPayAddr   []byte
	FWSSAddr     []byte
	FWSSViewAddr []byte
	PDPAddr      []byte
	RegistryAddr []byte

	// Wallet ETH addresses (20-byte slices)
	DeployerEthAddr []byte
	ClientEthAddr   []byte
	SPEthAddr       []byte

	// Private keys (raw 32-byte)
	DeployerKey []byte
	ClientKey   []byte
	SPKey       []byte

	// Curio PDP API URL
	CurioPDPURL string
}

// ParseEnvironment reads /shared/environment.env (written by filwizard) and
// /shared/curio/private_key (written by Curio init). Returns nil if the
// environment file does not exist, meaning the FOC compose profile is not active.
func ParseEnvironment() *Config {
	data, err := os.ReadFile("/shared/environment.env")
	if err != nil {
		return nil // FOC profile not active
	}

	env := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			env[parts[0]] = parts[1]
		}
	}

	cfg := &Config{}
	cfg.USDFCAddr = ParseEthAddrHex(env["USDFC_ADDRESS"])
	cfg.FilPayAddr = ParseEthAddrHex(env["FILECOIN_PAY_ADDRESS"])
	cfg.FWSSAddr = ParseEthAddrHex(env["FWSS_PROXY_ADDRESS"])
	cfg.FWSSViewAddr = ParseEthAddrHex(env["FWSS_VIEW_ADDRESS"])
	cfg.PDPAddr = ParseEthAddrHex(env["PDP_VERIFIER_PROXY_ADDRESS"])
	cfg.RegistryAddr = ParseEthAddrHex(env["SERVICE_PROVIDER_REGISTRY_PROXY_ADDRESS"])

	cfg.DeployerKey = ParseHexKey(env["DEPLOYER_PRIVATE_KEY"])
	cfg.ClientKey = ParseHexKey(env["CLIENT_PRIVATE_KEY"])

	if cfg.ClientKey != nil {
		cfg.ClientEthAddr = DeriveEthAddr(cfg.ClientKey)
	} else {
		cfg.ClientEthAddr = ParseEthAddrHex(env["CLIENT_ETH_ADDRESS"])
	}
	if cfg.DeployerKey != nil {
		cfg.DeployerEthAddr = DeriveEthAddr(cfg.DeployerKey)
	} else {
		cfg.DeployerEthAddr = ParseEthAddrHex(env["DEPLOYER_ETH_ADDRESS"])
	}

	// SP key lives in a separate file written by Curio (raw hex, no 0x prefix).
	// The curio data volume is mounted at /var/lib/curio in both curio and workload containers.
	cfg.loadSPKey()

	// Curio PDP URL
	if v := os.Getenv("CURIO_PDP_URL"); v != "" {
		cfg.CurioPDPURL = v
	} else {
		cfg.CurioPDPURL = "http://curio:80"
	}

	if cfg.FilPayAddr == nil {
		log.Printf("[foc] environment.env found but FILECOIN_PAY_ADDRESS missing or invalid")
		return nil
	}
	if cfg.USDFCAddr == nil {
		log.Printf("[foc] WARN: USDFC_ADDRESS missing — token invariant assertions will be skipped")
	}

	log.Printf("[foc] FOC environment loaded: USDFC=%x FilPay=%x FWSS=%x FWSSView=%x PDP=%x Registry=%x SP=%x client=%x deployer=%x",
		cfg.USDFCAddr, cfg.FilPayAddr, cfg.FWSSAddr, cfg.FWSSViewAddr, cfg.PDPAddr, cfg.RegistryAddr, cfg.SPEthAddr, cfg.ClientEthAddr, cfg.DeployerEthAddr)
	return cfg
}

// spKeyPaths lists candidate paths for the SP private key file.
// The curio data volume may be mounted at different paths depending on the container.
var spKeyPaths = []string{
	"/var/lib/curio/private_key",      // curio container (filecoin_service template)
	"/root/devgen/curio/private_key",  // workload container
	"/shared/curio/private_key",       // filwizard container
}

func (cfg *Config) loadSPKey() {
	for _, path := range spKeyPaths {
		spData, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		spHex := strings.TrimSpace(string(spData))
		cfg.SPKey = ParseHexKey(spHex)
		if cfg.SPKey != nil {
			cfg.SPEthAddr = DeriveEthAddr(cfg.SPKey)
			log.Printf("[foc] SP key loaded from %s", path)
			return
		}
		log.Printf("[foc] SP key file at %s had invalid content (len=%d)", path, len(spHex))
	}
	log.Printf("[foc] WARN: SP key not found at any known path — CreateDataSet and SP-signed actions will be unavailable")
}

// ReloadSPKey retries loading the SP key from disk (for lazy loading after startup).
func (cfg *Config) ReloadSPKey() {
	if cfg.SPKey != nil {
		return
	}
	cfg.loadSPKey()
}

// ParseEthAddrHex parses a 0x-prefixed hex Ethereum address into 20 bytes.
func ParseEthAddrHex(s string) []byte {
	s = strings.TrimPrefix(strings.ToLower(s), "0x")
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 20 {
		return nil
	}
	return b
}

// ParseHexKey parses a hex-encoded 32-byte private key (with or without 0x prefix).
func ParseHexKey(s string) []byte {
	s = strings.TrimPrefix(strings.ToLower(s), "0x")
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 32 {
		return nil
	}
	return b
}

// DeriveEthAddr derives the Ethereum address from a raw 32-byte secp256k1 private key.
func DeriveEthAddr(privKey []byte) []byte {
	pk := secp256k1.PrivKeyFromBytes(privKey)
	pub := pk.PubKey().SerializeUncompressed() // 65 bytes: 0x04 + X + Y
	addr, err := ethtypes.EthAddressFromPubKey(pub)
	if err != nil {
		log.Printf("[foc] DeriveEthAddr failed: %v", err)
		return nil
	}
	return addr
}

// DeriveFilAddr derives the Filecoin f4 (delegated) address from a secp256k1 private key.
func DeriveFilAddr(privKey []byte) (address.Address, error) {
	ethAddrBytes := DeriveEthAddr(privKey)
	if ethAddrBytes == nil {
		return address.Undef, fmt.Errorf("DeriveEthAddr returned nil")
	}
	ea, err := ethtypes.CastEthAddress(ethAddrBytes)
	if err != nil {
		return address.Undef, err
	}
	return ea.ToFilecoinAddress()
}
