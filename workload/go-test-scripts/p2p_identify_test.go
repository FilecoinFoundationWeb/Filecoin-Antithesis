//go:build go1.18
// +build go1.18

package main

import (
	"context"
	"math/rand"
	"os"
	"testing"
	"time"

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
	mutationType := rand.Intn(3)
	pos := rand.Intn(len(runes))
	switch mutationType {
	case 0: // Insertion
		char := rune(rand.Intn(94) + 33) // printable ASCII from '!' (33) to '~' (126)
		newRunes := append(runes[:pos], append([]rune{char}, runes[pos:]...)...)
		runes = newRunes
	case 1: // Deletion
		if len(runes) > 1 {
			runes = append(runes[:pos], runes[pos+1:]...)
		}
	case 2: // Substitution
		runes[pos] = rune(rand.Intn(94) + 33)
	}
	return string(runes)
}

// randomString returns a random string of length n using completely random bytes (0–255).
func randomString(n int) string {
	b := make([]byte, n)
	for i := 0; i < n; i++ {
		b[i] = byte(rand.Intn(256))
	}
	return string(b)
}

// TestSpamInvalidIdentifyPush repeatedly sends Identify push messages with a variety of mutated ObservedAddr values.
// It uses both mutated valid multiaddrs and completely random strings to cover more invalid and edge-case inputs.
func TestSpamInvalidIdentifyPush(t *testing.T) {
	// Get the target Lotus node multiaddr from the environment.
	target, ok := os.LookupEnv("LOTUS_TARGET")
	if !ok {
		t.Skip("LOTUS_TARGET environment variable not set")
	}

	// Seed corpus of valid multiaddr strings.
	seeds := []string{
		"/ip4/127.0.0.1/tcp/4001",
		"/ip4/192.168.1.1/tcp/1234",
		"/ip4/10.0.0.1/tcp/4001",
		"/ip4/10.0.0.2/tcp/4001",
		"/ip6/::1/tcp/4001",
		"/ip6/::2/tcp/4001",
		"/ip6zone/0",
	}

	// Create a sender libp2p host.
	ctx := context.Background()
	sender, err := libp2p.New()
	if err != nil {
		t.Fatalf("failed to create sender host: %v", err)
	}
	defer sender.Close()

	// Parse the target multiaddr.
	targetMaddr, err := ma.NewMultiaddr(target)
	if err != nil {
		t.Skip("failed to parse LOTUS_TARGET multiaddr")
	}
	ai, err := peer.AddrInfoFromP2pAddr(targetMaddr)
	if err != nil {
		t.Skip("failed to extract peer info from LOTUS_TARGET multiaddr")
	}

	// Connect the sender to the Lotus node.
	if err := sender.Connect(ctx, *ai); err != nil {
		t.Skip("failed to connect to Lotus node: " + err.Error())
	}

	// Allow a brief moment for connection establishment.
	time.Sleep(100 * time.Millisecond)

	// Number of messages to send.
	iterations := 200 // Adjust iteration count as needed.
	rand.Seed(time.Now().UnixNano())

	for i := 0; i < iterations; i++ {
		var payload string
		// 50% chance: mutate a valid seed; 50% chance: generate a completely random string.
		if rand.Float32() < 0.5 {
			seed := seeds[rand.Intn(len(seeds))]
			payload = mutateString(seed)
		} else {
			payload = randomString(rand.Intn(50) + 1) // random string length between 1 and 50.
		}

		// Open a new stream using the Identify push protocol.
		stream, err := sender.NewStream(ctx, ai.ID, identify.IDPush)
		if err != nil {
			t.Errorf("iteration %d: failed to open stream: %v", i, err)
			continue
		}

		writer := protoio.NewDelimitedWriter(stream)
		msg := &pb.Identify{
			ObservedAddr: []byte(payload),
		}
		t.Logf("iteration %d: sending message with ObservedAddr: %q", i, payload)
		if err := writer.WriteMsg(msg); err != nil {
			t.Errorf("iteration %d: failed to write Identify message: %v", i, err)
			stream.Reset()
			t.Logf("Message: %v", msg)
			continue
		}
		if err := stream.CloseWrite(); err != nil {
			t.Errorf("iteration %d: failed to close stream write: %v", i, err)
		}
		stream.Close()

		// Optional: Short pause between messages.
		time.Sleep(10 * time.Millisecond)
	}
}
