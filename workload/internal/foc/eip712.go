package foc

import (
	"fmt"
	"math/big"

	secp256k1 "github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"golang.org/x/crypto/sha3"
)

// EIP-712 type hashes matching SignatureVerificationLib.sol
var (
	MetadataEntryTypehash = Keccak256([]byte("MetadataEntry(string key,string value)"))
	CreateDataSetTypehash = Keccak256([]byte(
		"CreateDataSet(uint256 clientDataSetId,address payee,MetadataEntry[] metadata)MetadataEntry(string key,string value)",
	))
	// EIP-712 domain type hash
	EIP712DomainTypehash = Keccak256([]byte(
		"EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)",
	))
)

// Keccak256 computes the keccak256 hash of data.
func Keccak256(data []byte) []byte {
	h := sha3.NewLegacyKeccak256()
	h.Write(data)
	return h.Sum(nil)
}

// BuildDomainSeparator builds the EIP-712 domain separator for FWSS.
func BuildDomainSeparator(fwssAddr []byte) []byte {
	nameHash := Keccak256([]byte("FilecoinWarmStorageService"))
	versionHash := Keccak256([]byte("1"))
	chainID := new(big.Int).SetInt64(31415926)

	encoded := make([]byte, 0, 32*5)
	encoded = append(encoded, EIP712DomainTypehash...)
	encoded = append(encoded, nameHash...)
	encoded = append(encoded, versionHash...)
	encoded = append(encoded, EncodeBigInt(chainID)...)
	encoded = append(encoded, EncodeAddress(fwssAddr)...)

	return Keccak256(encoded)
}

// HashMetadataEntry hashes a single metadata entry for EIP-712.
func HashMetadataEntry(key, value string) []byte {
	keyHash := Keccak256([]byte(key))
	valueHash := Keccak256([]byte(value))

	encoded := make([]byte, 0, 32*3)
	encoded = append(encoded, MetadataEntryTypehash...)
	encoded = append(encoded, keyHash...)
	encoded = append(encoded, valueHash...)

	return Keccak256(encoded)
}

// HashMetadataEntries hashes an array of metadata entries.
func HashMetadataEntries(keys, values []string) []byte {
	if len(keys) == 0 {
		return Keccak256(nil)
	}
	packed := make([]byte, 0, 32*len(keys))
	for i := range keys {
		packed = append(packed, HashMetadataEntry(keys[i], values[i])...)
	}
	return Keccak256(packed)
}

// CreateDataSetStructHash builds the EIP-712 struct hash for CreateDataSet.
func CreateDataSetStructHash(clientDataSetId *big.Int, payee []byte, metadataKeys, metadataValues []string) []byte {
	metadataHash := HashMetadataEntries(metadataKeys, metadataValues)

	encoded := make([]byte, 0, 32*4)
	encoded = append(encoded, CreateDataSetTypehash...)
	encoded = append(encoded, EncodeBigInt(clientDataSetId)...)
	encoded = append(encoded, EncodeAddress(payee)...)
	encoded = append(encoded, metadataHash...)

	return Keccak256(encoded)
}

// BuildEIP712Digest builds the full EIP-712 digest: keccak256("\x19\x01" + domainSeparator + structHash)
func BuildEIP712Digest(domainSeparator, structHash []byte) []byte {
	msg := make([]byte, 0, 2+32+32)
	msg = append(msg, 0x19, 0x01)
	msg = append(msg, domainSeparator...)
	msg = append(msg, structHash...)
	return Keccak256(msg)
}

// SignEIP712CreateDataSet signs a CreateDataSet EIP-712 message with the given private key.
// Returns the 65-byte signature (r[32] + s[32] + v[1]).
func SignEIP712CreateDataSet(privKey []byte, fwssAddr []byte, clientDataSetId *big.Int, payee []byte, metadataKeys, metadataValues []string) ([]byte, error) {
	domainSep := BuildDomainSeparator(fwssAddr)
	structHash := CreateDataSetStructHash(clientDataSetId, payee, metadataKeys, metadataValues)
	digest := BuildEIP712Digest(domainSep, structHash)

	return Secp256k1Sign(privKey, digest)
}

// Secp256k1Sign signs a 32-byte hash with a raw secp256k1 private key.
// Returns 65-byte signature in Ethereum format: R (32) + S (32) + V (1).
// V is 0 or 1 (recovery id, not 27/28 — the contract handles adjustment).
func Secp256k1Sign(privKey []byte, hash []byte) ([]byte, error) {
	if len(privKey) != 32 || len(hash) != 32 {
		return nil, fmt.Errorf("invalid key or hash length")
	}

	pk := secp256k1.PrivKeyFromBytes(privKey)

	// SignCompact returns [v, r, s] where v is the recovery ID + 27
	sigBytes := ecdsa.SignCompact(pk, hash, false)
	// sigBytes[0] = recovery ID + 27 (i.e., 27 or 28)
	// sigBytes[1:33] = R
	// sigBytes[33:65] = S

	// Convert to Ethereum format: R + S + V (where V = 0 or 1)
	result := make([]byte, 65)
	copy(result[0:32], sigBytes[1:33])   // R
	copy(result[32:64], sigBytes[33:65]) // S
	result[64] = sigBytes[0] - 27        // V (recovery ID: 0 or 1)

	return result, nil
}
