package mpoolfuzz

import (
	"crypto/rand"
	"encoding/binary"
	"math/big"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/lotus/chain/types"
)

func CreateBaseMessage(from, to address.Address, _ uint64) *types.Message {
	return &types.Message{
		From:       from,
		To:         to,
		Value:      types.NewInt(100000000000000), // 0.0001 FIL in attoFIL
		GasLimit:   1000000,
		GasFeeCap:  types.NewInt(1000000000), // 1 nanoFIL in attoFIL
		GasPremium: types.NewInt(1000000000), // 1 nanoFIL in attoFIL
		Method:     0,
		Params:     nil,
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

func GenerateRandomAddress() (address.Address, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return address.Undef, err
	}
	return address.NewIDAddress(n.Uint64())
}
