package resources

import (
	context "context"
	cryptoRand "crypto/rand"
	"encoding/binary"
	"log"
	"math"
	"math/big"
	mathRand "math/rand"
	"sync"
	"time"

	"github.com/antithesishq/antithesis-sdk-go/assert"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
)

var (
	storedSignedMessages []*types.SignedMessage
	storedMutex          sync.Mutex
)

func SendInvalidTransactions(ctx context.Context, api api.FullNode, from address.Address, to address.Address, count int) error {
	log.Printf("Starting invalid transaction fuzzing with %d transactions", count)
	mathRand.Seed(time.Now().UnixNano())
	attackStrategy := mathRand.Intn(5)

	switch attackStrategy {
	case 0:
		log.Printf("Strategy: Standard mutation attacks")
		return sendStandardMutations(ctx, api, from, to, count)
	case 1:
		log.Printf("Strategy: Chained transaction attacks")
		return sendChainedTransactions(ctx, api, from, count)
	case 2:
		log.Printf("Strategy: Burst with concurrent requests")
		return sendConcurrentBurst(ctx, api, from, to, count)
	case 3:
		log.Printf("Strategy: Mixed subtle attacks")
		return sendSubtleAttacks(ctx, api, from, to, count)
	case 4:
		log.Printf("Strategy: Protocol edge cases")
		return sendProtocolEdgeCases(ctx, api, from, to, count)
	default:
		log.Printf("Strategy: Fallback to standard mutations")
		return sendStandardMutations(ctx, api, from, to, count)
	}
}

func sendStandardMutations(ctx context.Context, api api.FullNode, from address.Address, to address.Address, count int) error {
	startingNonce, err := api.MpoolGetNonce(ctx, from)
	if err != nil {
		log.Printf("[WARN] Could not get nonce for %s: %v, using 0", from, err)
		startingNonce = 0
	}

	for i := 0; i < count; i++ {
		msg := &types.Message{
			To:         to,
			From:       from,
			Nonce:      startingNonce + uint64(i),
			Value:      abi.NewTokenAmount(0),
			GasLimit:   100000 + mathRand.Int63n(1000000),
			GasFeeCap:  abi.NewTokenAmount(1 + mathRand.Int63n(1e9)),
			GasPremium: abi.NewTokenAmount(1 + mathRand.Int63n(1e9)),
			Method:     abi.MethodNum(mathRand.Intn(100)),
			Params:     randomBytes(mathRand.Intn(64)),
		}

		switch i % 16 {
		case 0:
			msg.GasPremium = abi.NewTokenAmount(100)
			msg.GasFeeCap = abi.NewTokenAmount(1)
			log.Printf("[Test %d] GasPremium > GasFeeCap", i)
		case 1:
			msg.GasLimit = -1000
			log.Printf("[Test %d] Negative gas limit", i)
		case 2:
			raw := new(big.Int).SetUint64(^uint64(0))
			msg.Value = abi.TokenAmount{Int: raw}
			log.Printf("[Test %d] Maximum uint64 value", i)
		case 3:
			msg.Method = 99
			msg.Params = randomBytes(128)
			log.Printf("[Test %d] Garbage params for method 99", i)
		case 4:
			msg.To, _ = address.NewIDAddress(0)
			log.Printf("[Test %d] Reserved ID address 0", i)
		case 5:
			msg.Params = randomBytes(2048)
			log.Printf("[Test %d] Oversized params (2KB)", i)
		case 6:
			msg.Params = []byte{0xff, 0x01, 0x02, 0x03}
			log.Printf("[Test %d] Malformed CBOR", i)
		case 7:
			msg.Params = []byte{0xa1, 0x63, 0x6a, 0x75, 0x6e, 0x6b, 0x58, 0x20}
			log.Printf("[Test %d] CBOR junk", i)
		case 8:
			msg.Version = 2
			log.Printf("[Test %d] Unsupported message version", i)
		case 9:
			msg.GasLimit = 1 << 60
			log.Printf("[Test %d] Unrealistically high gas limit", i)
		case 10:
			msg.Method = 5
			msg.Params = []byte{}
			log.Printf("[Test %d] Empty params with non-zero method", i)
		case 11:
			msg.Params = []byte("☢️💥🔥")
			log.Printf("[Test %d] Unicode params", i)
		case 12:
			msg.To = from
			msg.Method = 12
			log.Printf("[Test %d] Self-referential actor call", i)
		case 13:
			msg.GasFeeCap = abi.NewTokenAmount(math.MaxInt64)
			msg.GasPremium = abi.NewTokenAmount(math.MaxInt64)
			log.Printf("[Test %d] Integer overflow gas parameters", i)
		case 14:
			msg.Params = []byte{0x80}
			msg.Method = 6
			log.Printf("[Test %d] Minimal valid CBOR with unexpected method", i)
		case 15:
			// Try with nearly zero values for gas
			msg.GasFeeCap = abi.NewTokenAmount(1)
			msg.GasPremium = abi.NewTokenAmount(1)
			msg.GasLimit = 1
			log.Printf("[Test %d] Minimal gas values", i)
		}

		// Regular push for all cases
		smsg, err := api.MpoolPushMessage(ctx, msg, nil)
		assert.Sometimes(err != nil, "Invalid message handling", map[string]any{
			"iteration": i,
			"from":      from.String(),
			"to":        to.String(),
			"error":     err,
		})
		if err != nil {
			log.Printf("[rejected] tx %d: %v", i, err)
		} else {
			log.Printf("[ACCEPTED] tx %d was accepted: %s", i, smsg.Cid())
			storedMutex.Lock()
			storedSignedMessages = append(storedSignedMessages, smsg)
			storedMutex.Unlock()
		}

		delay := time.Millisecond * time.Duration(50+mathRand.Intn(200))
		time.Sleep(delay)
	}

	if len(storedSignedMessages) > 2 {
		log.Printf("[INFO] Testing MpoolPush with manually modified signed messages")

		originalMsg := storedSignedMessages[0]
		modifiedMsg := *originalMsg

		modifiedMsg.Message.GasLimit = 1000000

		_, err := api.MpoolPush(ctx, &modifiedMsg)
		log.Printf("[INFO] Pushing tampered message result: %v", err)
		assert.Sometimes(err != nil, "Tampered message handling", map[string]any{
			"error": err,
		})
	}

	if len(storedSignedMessages) > 0 {
		ReplayStoredSignedMessages(ctx, api)
	}

	return nil
}

func sendChainedTransactions(ctx context.Context, api api.FullNode, from address.Address, count int) error {
	nonce, err := api.MpoolGetNonce(ctx, from)
	if err != nil {
		log.Printf("[WARN] Could not get nonce for %s: %v, using 0", from, err)
		nonce = 0
	}

	validMsg := &types.Message{
		To:         from,
		From:       from,
		Nonce:      nonce,
		Value:      abi.NewTokenAmount(0),
		GasLimit:   1000000,
		GasFeeCap:  abi.NewTokenAmount(1e9),
		GasPremium: abi.NewTokenAmount(1e8),
		Method:     0,
		Params:     []byte{},
	}

	validSigned, err := api.MpoolPushMessage(ctx, validMsg, nil)
	assert.Sometimes(err != nil, "Invalid message handling", map[string]any{
		"iteration": 0,
		"from":      from.String(),
		"to":        from.String(),
		"error":     err,
	})
	if err != nil {
		log.Printf("[ERROR] Failed to send initial valid message: %v", err)
		return sendStandardMutations(ctx, api, from, from, count)
	}

	log.Printf("[INFO] Sent initial valid message with CID: %s", validSigned.Cid())
	storedMutex.Lock()
	storedSignedMessages = append(storedSignedMessages, validSigned)
	storedMutex.Unlock()

	time.Sleep(time.Millisecond * 500)

	for i := 1; i < count; i++ {
		msg := &types.Message{
			To:         from,
			From:       from,
			Nonce:      nonce + uint64(i),
			Value:      abi.NewTokenAmount(0),
			GasLimit:   1000000,
			GasFeeCap:  abi.NewTokenAmount(1e9),
			GasPremium: abi.NewTokenAmount(1e8),
			Method:     0,
			Params:     []byte{},
		}

		switch i % 5 {
		case 0:
			msg.Params = []byte{0x01}
			log.Printf("[Chain %d] Normal looking with subtle params issue", i)
		case 1:
			msg.GasLimit = 10000000000
			log.Printf("[Chain %d] Extremely high gas limit", i)
		case 2:
			msg.Method = 99
			log.Printf("[Chain %d] Invalid method number", i)
		case 3:
			msg.Method = 2
			msg.Params = createMalformedCBOR(32)
			log.Printf("[Chain %d] Malformed CBOR params", i)
		case 4:
			randomAddr, err := address.NewIDAddress(uint64(mathRand.Intn(100) + 100))
			if err != nil {
				randomAddr = from
			}
			msg.To = randomAddr
			msg.Value = abi.NewTokenAmount(1)
			log.Printf("[Chain %d] Transfer to random address %s", i, randomAddr)
		}

		smsg, err := api.MpoolPushMessage(ctx, msg, nil)
		assert.Sometimes(err != nil, "Invalid message handling", map[string]any{
			"iteration": i,
			"from":      from.String(),
			"to":        from.String(),
			"error":     err,
		})
		if err != nil {
			log.Printf("[rejected] Chain tx %d: %v", i, err)
		} else {
			log.Printf("[ACCEPTED] Chain tx %d was accepted: %s", i, smsg.Cid())
			storedMutex.Lock()
			storedSignedMessages = append(storedSignedMessages, smsg)
			storedMutex.Unlock()
		}

		delay := time.Millisecond * time.Duration(100+mathRand.Intn(400))
		time.Sleep(delay)
	}

	return nil
}

func sendConcurrentBurst(ctx context.Context, api api.FullNode, from address.Address, to address.Address, count int) error {
	nonce, err := api.MpoolGetNonce(ctx, from)
	if err != nil {
		log.Printf("[WARN] Could not get nonce for %s: %v, using 0", from, err)
		nonce = 0
	}

	nonce = 0

	results := make(chan struct {
		index int
		err   error
		msg   *types.SignedMessage
	}, count)

	messages := make([]*types.Message, count)
	for i := 0; i < count; i++ {
		msg := &types.Message{
			To:         to,
			From:       from,
			Nonce:      nonce + uint64(i),
			Value:      abi.NewTokenAmount(mathRand.Int63n(100)),
			GasLimit:   100000 + mathRand.Int63n(1000000),
			GasFeeCap:  abi.NewTokenAmount(1 + mathRand.Int63n(1e9)),
			GasPremium: abi.NewTokenAmount(1 + mathRand.Int63n(1e9)),
			Method:     abi.MethodNum(mathRand.Intn(5)),
			Params:     randomBytes(mathRand.Intn(32)),
		}

		if i%3 == 0 {
			msg.GasLimit = -1
		} else if i%7 == 0 {
			msg.GasPremium = abi.NewTokenAmount(1e18)
		}

		messages[i] = msg
	}

	log.Printf("[BURST] Sending %d concurrent messages", count)

	go func() {
		accepted := 0
		rejected := 0

		for res := range results {
			if res.err != nil {
				rejected++
				log.Printf("[BURST %d/%d] Rejected: %v", rejected, count, res.err)
			} else {
				accepted++
				log.Printf("[BURST %d/%d] Accepted: %s", accepted, count, res.msg.Cid())
				storedMutex.Lock()
				storedSignedMessages = append(storedSignedMessages, res.msg)
				storedMutex.Unlock()
			}
		}

		log.Printf("[BURST COMPLETE] %d accepted, %d rejected", accepted, rejected)
	}()

	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)

		go func(idx int) {
			defer wg.Done()
			time.Sleep(time.Millisecond * time.Duration(mathRand.Intn(50)))
			smsg, err := api.MpoolPushMessage(ctx, messages[idx], nil)
			results <- struct {
				index int
				err   error
				msg   *types.SignedMessage
			}{idx, err, smsg}
		}(i)
	}

	wg.Wait()
	close(results)

	return nil
}

func sendSubtleAttacks(ctx context.Context, api api.FullNode, from address.Address, to address.Address, count int) error {
	nonce, err := api.MpoolGetNonce(ctx, from)
	if err != nil {
		log.Printf("[WARN] Could not get nonce for %s: %v, using 0", from, err)
		nonce = 0
	}

	head, err := api.ChainHead(ctx)
	if err != nil {
		log.Printf("[WARN] Could not get chain head: %v", err)
	}

	for i := 0; i < count; i++ {
		msg := &types.Message{
			To:         to,
			From:       from,
			Nonce:      nonce,
			Value:      abi.NewTokenAmount(1),
			GasLimit:   1000000,
			GasFeeCap:  abi.NewTokenAmount(1e9),
			GasPremium: abi.NewTokenAmount(1e8),
			Method:     0,
			Params:     []byte{},
		}

		subtleIssue := i % 10
		switch subtleIssue {
		case 0:
			msg.Params = []byte{0x00}
			log.Printf("[Subtle %d] Transfer with non-empty params", i)
		case 1:
			msg.GasLimit = 21000
			log.Printf("[Subtle %d] Minimal gas limit", i)
		case 2:
			msg.Value = abi.NewTokenAmount(1000000000000001)
			log.Printf("[Subtle %d] Value greater than balance", i)
		case 3:
			if head != nil {
				actors, err := api.StateListActors(ctx, head.Key())
				if err == nil && len(actors) > 0 {
					randomIndex := mathRand.Intn(len(actors))
					msg.To = actors[randomIndex]
					msg.Method = 1
					log.Printf("[Subtle %d] Actual actor %s with method 1", i, msg.To)
				}
			}
		case 4:
			msg.Value = abi.NewTokenAmount(1)
			log.Printf("[Subtle %d] Minimal value transfer", i)
		case 5:
			msg.Method = 2
			msg.Params = []byte{0xa1, 0x61, 0x01, 0x01}
			log.Printf("[Subtle %d] Plausible-looking but invalid CBOR", i)
		case 6:
			msg.GasFeeCap = abi.NewTokenAmount(1e12)
			msg.GasPremium = abi.NewTokenAmount(1e11)
			log.Printf("[Subtle %d] Excessive gas price", i)
		case 7:
			msg.To = from
			log.Printf("[Subtle %d] Self-transfer", i)
		case 8:
			msg.Method = 3
			msg.Params = []byte{0x80}
			log.Printf("[Subtle %d] Valid params for wrong method", i)
		case 9:
			msg.Value = abi.NewTokenAmount(1000000000000001)
			log.Printf("[Subtle %d] Value exceeds balance", i)
		}

		smsg, err := api.MpoolPushMessage(ctx, msg, nil)
		if err != nil {
			log.Printf("[rejected] Subtle tx %d: %v", i, err)
		} else {
			log.Printf("[ACCEPTED] Subtle tx %d was accepted: %s", i, smsg.Cid())
			storedMutex.Lock()
			storedSignedMessages = append(storedSignedMessages, smsg)
			storedMutex.Unlock()
			nonce++
		}

		time.Sleep(time.Millisecond * time.Duration(100+mathRand.Intn(200)))
	}

	return nil
}

func sendProtocolEdgeCases(ctx context.Context, api api.FullNode, from address.Address, to address.Address, count int) error {
	nonce, err := api.MpoolGetNonce(ctx, from)
	if err != nil {
		log.Printf("[WARN] Could not get nonce for %s: %v, using 0", from, err)
		nonce = 0
	}

	edgeCases := []struct {
		name        string
		mutateMsg   func(*types.Message)
		description string
	}{
		{
			name: "Zero gas limit",
			mutateMsg: func(msg *types.Message) {
				msg.GasLimit = 0
			},
			description: "Message with zero gas limit",
		},
		{
			name: "Negative value",
			mutateMsg: func(msg *types.Message) {
				negative := big.NewInt(-1)
				msg.Value = abi.TokenAmount{Int: negative}
			},
			description: "Message with negative value",
		},
		{
			name: "Method overflow",
			mutateMsg: func(msg *types.Message) {
				msg.Method = 1<<32 - 1
			},
			description: "Message with maximum method number",
		},
		{
			name: "Enormous params",
			mutateMsg: func(msg *types.Message) {
				msg.Params = make([]byte, 1024*1024)
				_, _ = cryptoRand.Read(msg.Params)
			},
			description: "Message with 1MB of params",
		},
		{
			name: "Zero nonce valid message",
			mutateMsg: func(msg *types.Message) {
				msg.Nonce = 0
				msg.Method = 0
				msg.Params = []byte{}
				msg.GasLimit = 1000000
			},
			description: "Message with zero nonce but otherwise valid",
		},
	}

	for i := 0; i < count; i++ {
		edgeCase := edgeCases[i%len(edgeCases)]

		msg := &types.Message{
			To:         to,
			From:       from,
			Nonce:      nonce + uint64(i),
			Value:      abi.NewTokenAmount(1),
			GasLimit:   1000000,
			GasFeeCap:  abi.NewTokenAmount(1e9),
			GasPremium: abi.NewTokenAmount(1e8),
			Method:     0,
			Params:     []byte{},
		}

		edgeCase.mutateMsg(msg)
		log.Printf("[Edge %d] Testing: %s", i, edgeCase.description)

		smsg, err := api.MpoolPushMessage(ctx, msg, nil)
		assert.Sometimes(err != nil, "Invalid message handling", map[string]any{
			"iteration": i,
			"from":      from.String(),
			"to":        to.String(),
			"error":     err,
		})
		if err != nil {
			log.Printf("[rejected] Edge tx %d (%s): %v", i, edgeCase.name, err)
		} else {
			log.Printf("[ACCEPTED] Edge tx %d (%s) was accepted: %s", i, edgeCase.name, smsg.Cid())
			storedMutex.Lock()
			storedSignedMessages = append(storedSignedMessages, smsg)
			storedMutex.Unlock()
		}

		time.Sleep(time.Millisecond * 250)
	}

	return nil
}

func ReplayStoredSignedMessages(ctx context.Context, api api.FullNode) {
	storedMutex.Lock()
	messages := make([]*types.SignedMessage, len(storedSignedMessages))
	copy(messages, storedSignedMessages)
	storedMutex.Unlock()

	if len(messages) == 0 {
		log.Printf("[INFO] No stored messages to replay")
		return
	}

	log.Printf("[INFO] Replaying %d stored messages", len(messages))
	acceptCount := 0

	for i, smsg := range messages {
		_, err := api.MpoolPush(ctx, smsg)
		if err != nil {
			log.Printf("[OK] Replay rejected (tx %d): %v", i, err)
		} else {
			acceptCount++
			log.Printf("[BUG] Replay accepted (tx %d): %s", i, smsg.Cid())
		}

		time.Sleep(time.Millisecond * 100)
	}

	log.Printf("[SUMMARY] Replayed %d messages, %d were unexpectedly accepted", len(messages), acceptCount)
}

func randomBytes(n int) []byte {
	buf := make([]byte, n)
	_, _ = cryptoRand.Read(buf)
	return buf
}

func createMalformedCBOR(size int) []byte {
	buf := []byte{0xa1}
	key := randomBytes(4)
	value := randomBytes(size - 6)

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
