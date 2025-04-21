package mpoolfuzz

import (
	"crypto/rand"
	"encoding/binary"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/types"
)

func CreateBaseMessage(from, to address.Address, nonce uint64) *types.Message {
	return &types.Message{
		From:       from,
		To:         to,
		Nonce:      nonce,
		Value:      types.NewInt(0),
		GasLimit:   1000000,
		GasFeeCap:  abi.NewTokenAmount(1e9),
		GasPremium: abi.NewTokenAmount(1e9),
		Method:     0,
		Params:     []byte{},
	}
}

func RandomBytes(n int) []byte {
	buff := make([]byte, n)
	rand.Read(buff)
	return buff
}

func CreateMalformedCBOR(size int) []byte {
	buf := []byte{0xa1}

	key := RandomBytes(4)
	value := RandomBytes(size - 6)

	buf = append(buf, 0x58)
	buf = append(buf, byte(len(key)))
	buf = append(buf, key...)

	buf = append(buf, 0x59)
	length := make([]byte, 2)
	binary.BigEndian.PutUint16(length, uint16(len(value)))
	buf = append(buf, length...)
	buf = append(buf, value...)
	return buf
}
