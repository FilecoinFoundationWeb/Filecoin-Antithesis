//go:build go1.18
// +build go1.18

package main

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources"
	"github.com/antithesishq/antithesis-sdk-go/assert"
	"github.com/antithesishq/antithesis-sdk-go/random"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/protocol/identify"
	pb "github.com/libp2p/go-libp2p/p2p/protocol/identify/pb"
	"github.com/libp2p/go-msgio/protoio"
	ma "github.com/multiformats/go-multiaddr"
)

// mutateString randomly alters a string by inserting, deleting, or substituting a character.
func mutateString(s string) string {
	runes := []rune(s)
	if len(runes) == 0 {
		return s
	}
	mutationType := int(random.GetRandom() % 3)
	pos := int(random.GetRandom() % uint64(len(runes)))
	switch mutationType {
	case 0: // Insertion
		char := rune(random.GetRandom()%94 + 33) // printable ASCII from '!' (33) to '~' (126)
		newRunes := append(runes[:pos], append([]rune{char}, runes[pos:]...)...)
		runes = newRunes
	case 1: // Deletion
		if len(runes) > 1 {
			runes = append(runes[:pos], runes[pos+1:]...)
		}
	case 2: // Substitution
		runes[pos] = rune(random.GetRandom()%94 + 33)
	}
	return string(runes)
}

// randomString returns a random string of length n using completely random bytes (0–255).
func randomString(n int) string {
	b := make([]byte, n)
	for i := 0; i < n; i++ {
		b[i] = byte(random.GetRandom() % 256)
	}
	return string(b)
}

// TestSpamInvalidIdentifyPush repeatedly sends Identify push messages with a variety of mutated ObservedAddr values.
func TestSpamInvalidIdentifyPush(t *testing.T) {
	target, ok := os.LookupEnv("LOTUS_TARGET")
	if !ok {
		t.Skip("LOTUS_TARGET environment variable not set")
	}

	seeds := []string{
		"/ip4/127.0.0.1/tcp/4001",
		"/ip4/192.168.1.1/tcp/1234",
		"/ip4/10.0.0.1/tcp/4001",
		"/ip4/10.0.0.2/tcp/4001",
		"/ip6/::1/tcp/4001",
		"/ip6/::2/tcp/4001",
		"/ip6zone/0",
	}

	ctx := context.Background()
	sender, err := libp2p.New()
	if err != nil {
		t.Fatalf("failed to create sender host: %v", err)
	}
	defer sender.Close()

	targetMaddr, err := ma.NewMultiaddr(target)
	if err != nil {
		t.Fatalf("failed to parse LOTUS_TARGET multiaddr: %v", err)
	}

	ai, err := peer.AddrInfoFromP2pAddr(targetMaddr)
	if err != nil {
		t.Fatalf("failed to extract peer info from LOTUS_TARGET multiaddr: %v", err)
	}

	// Dial with timeout
	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	err = sender.Connect(dialCtx, *ai)
	if err != nil {
		t.Skipf("failed to connect to target: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	numIterations := 200
	successfulStreams := 0
	successfulWrites := 0

	for i := 0; i < numIterations; i++ {
		var payload string
		if random.GetRandom()%2 == 0 {
			seed := seeds[random.GetRandom()%uint64(len(seeds))]
			payload = mutateString(seed)
		} else {
			payload = randomString(int(random.GetRandom()%50 + 1))
		}

		stream, err := sender.NewStream(ctx, ai.ID, identify.IDPush)
		if err == nil {
			successfulStreams++
		}
		assert.Sometimes(err == nil,
			"[P2P Stream] Stream creation should succeed",
			resources.EnhanceAssertDetails(
				map[string]interface{}{
					"iteration": i,
					"peer":      ai.ID.String(),
					"error":     err,
					"property":  "P2P stream creation",
					"impact":    "High - required for peer communication",
					"details":   "Stream creation is essential for P2P message exchange",
					"protocol":  "identify.IDPush",
				},
				"p2p_test",
			))
		if err != nil {
			t.Logf("iteration %d: failed to open stream: %v", i, err)
			continue
		}

		writer := protoio.NewDelimitedWriter(stream)
		msg := &pb.Identify{ObservedAddr: []byte(payload)}

		err = writer.WriteMsg(msg)
		if err == nil {
			successfulWrites++
		}
		assert.Sometimes(err == nil,
			"[P2P Write] Message write should succeed",
			resources.EnhanceAssertDetails(
				map[string]interface{}{
					"iteration":   i,
					"payloadSize": len(payload),
					"error":       err,
					"property":    "P2P message writing",
					"impact":      "High - validates message transmission",
					"details":     "Message writing tests network data transfer capability",
				},
				"p2p_test",
			))
		if err != nil {
			t.Logf("iteration %d: write error: %v", i, err)
			stream.Reset()
			continue
		}

		err = stream.CloseWrite()
		assert.Sometimes(err == nil,
			"[P2P Close] Stream close should succeed",
			resources.EnhanceAssertDetails(
				map[string]interface{}{
					"iteration": i,
					"error":     err,
					"property":  "P2P stream closure",
					"impact":    "Medium - validates proper stream cleanup",
					"details":   "Clean stream closure prevents resource leaks",
				},
				"p2p_test",
			))
		if err != nil {
			t.Logf("iteration %d: close write error: %v", i, err)
		}
		stream.Close()
		time.Sleep(10 * time.Millisecond)
	}

	// Assert overall success rates
	assert.Sometimes(float64(successfulStreams)/float64(numIterations) > 0.5,
		"[P2P Success Rate] Stream creation success rate should be above threshold",
		resources.EnhanceAssertDetails(
			map[string]interface{}{
				"successRate": float64(successfulStreams) / float64(numIterations),
				"successful":  successfulStreams,
				"total":       numIterations,
				"property":    "P2P stream reliability",
				"impact":      "Critical - indicates network stability",
				"details":     "Success rate below 50% may indicate network issues",
				"threshold":   0.5,
			},
			"p2p_test",
		))
	assert.Sometimes(float64(successfulWrites)/float64(numIterations) > 0.3,
		"[P2P Success Rate] Message write success rate should be above threshold",
		resources.EnhanceAssertDetails(
			map[string]interface{}{
				"successRate": float64(successfulWrites) / float64(numIterations),
				"successful":  successfulWrites,
				"total":       numIterations,
				"property":    "P2P write reliability",
				"impact":      "Critical - indicates data transfer stability",
				"details":     "Success rate below 30% may indicate serious network issues",
				"threshold":   0.3,
			},
			"p2p_test",
		))
}
