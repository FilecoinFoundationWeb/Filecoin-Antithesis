package main

import (
	"context"
	"log"
	"os"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/protocol/identify"
	"github.com/libp2p/go-libp2p/p2p/protocol/identify/pb"
	"github.com/libp2p/go-msgio/protoio"
)

func FuzzIdentifyObservedAddr(f *testing.F) {
	targetAddr := os.Getenv("TARGET_ADDR")
	if targetAddr == "" {
		f.Skip("TARGET_MULTIADDR environment variable not set; skipping fuzz test")
	}

	h, err := libp2p.New()
	log.Printf("libp2p host created with ID: %s", h.ID())
	if err != nil {
		f.Fatalf("failed to create libp2p host: %v", err)
	}
	// Parse the target node’s address.
	ai, err := peer.AddrInfoFromString(targetAddr)
	if err != nil {
		f.Fatalf("failed to parse target multiaddr %q: %v", targetAddr, err)
	}
	// Connect to the target Lotus node.
	if err := h.Connect(context.Background(), *ai); err != nil {
		f.Fatalf("failed to connect to target %q: %v", targetAddr, err)
	}
	log.Printf("connected to target %q", targetAddr)
	f.Add([]byte("/ip6zone/0"))
	f.Add([]byte("/ip4/10.20.20.30/tcp/1234"))

	f.Fuzz(func(t *testing.T, data []byte) {
		str, err := h.NewStream(context.Background(), ai.ID, identify.IDPush)
		if err != nil {
			t.Fatalf("failed to create stream: %v", err)
		}
		defer str.Close()
		msg := &pb.Identify{
			ObservedAddr: data,
		}
		w := protoio.NewDelimitedWriter(str)
		if err := w.WriteMsg(msg); err != nil {
			t.Errorf("failed to write Identify message: %v", err)
			return
		}
		// Close the write side of the stream.
		if err := str.CloseWrite(); err != nil {
			t.Errorf("failed to close stream write: %v", err)
			return
		}
		time.Sleep(20 * time.Millisecond)
	})
}
