package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"time"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api/client"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/node/modules/dtypes"

	"github.com/libp2p/go-libp2p"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	pb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/minio/blake2b-simd"
)

const (
	GossipScoreThreshold   = -500
	PublishScoreThreshold  = -1000
	GraylistScoreThreshold = -2500
	BlockTopicFormat       = "/fil/blocks/%s"
	MessageTopicFormat     = "/fil/msgs/%s"
)

type PubsubClient struct {
	host    host.Host
	pubsub  *pubsub.PubSub
	topics  map[string]*pubsub.Topic
	network dtypes.NetworkName
}

// HashMsgId now matches the expected signature: func(*pb.Message) string
func HashMsgId(pmsg *pb.Message) string {
	sum := blake2b.Sum256(pmsg.Data)
	return string(sum[:])
}

func NewPubsubClient() (*PubsubClient, error) {
	networkName := os.Getenv("LOTUS_NETWORK")
	if networkName == "" {
		networkName = "mainnet"
		log.Printf("LOTUS_NETWORK not set, defaulting to %s", networkName)
	}

	// Configure pubsub parameters
	pubsub.GossipSubD = 8
	pubsub.GossipSubDscore = 6
	pubsub.GossipSubDout = 3
	pubsub.GossipSubDlo = 6
	pubsub.GossipSubDhi = 12
	pubsub.GossipSubDlazy = 12
	pubsub.GossipSubDirectConnectInitialDelay = 30 * time.Second
	pubsub.GossipSubIWantFollowupTime = 5 * time.Second
	pubsub.GossipSubHistoryLength = 10
	pubsub.GossipSubGossipFactor = 0.1

	// Create libp2p host
	h, err := libp2p.New(
		libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/0"),
		libp2p.DisableRelay(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create libp2p host: %w", err)
	}

	// Create pubsub instance with correct message ID function
	ps, err := pubsub.NewGossipSub(
		context.Background(),
		h,
		pubsub.WithMessageIdFn(HashMsgId),
		pubsub.WithPeerScore(
			&pubsub.PeerScoreParams{
				AppSpecificScore: func(p peer.ID) float64 {
					return 0
				},
				AppSpecificWeight:           1,
				IPColocationFactorWeight:    -100,
				IPColocationFactorThreshold: 5,
				BehaviourPenaltyThreshold:   6,
				BehaviourPenaltyWeight:      -10,
				BehaviourPenaltyDecay:       pubsub.ScoreParameterDecay(time.Hour),
				DecayInterval:               pubsub.DefaultDecayInterval,
				DecayToZero:                 pubsub.DefaultDecayToZero,
				RetainScore:                 6 * time.Hour,
			},
			&pubsub.PeerScoreThresholds{
				GossipThreshold:             GossipScoreThreshold,
				PublishThreshold:            PublishScoreThreshold,
				GraylistThreshold:           GraylistScoreThreshold,
				AcceptPXThreshold:           1000,
				OpportunisticGraftThreshold: 3.5,
			},
		),
	)
	if err != nil {
		h.Close()
		return nil, fmt.Errorf("failed to create pubsub: %w", err)
	}

	return &PubsubClient{
		host:    h,
		pubsub:  ps,
		topics:  make(map[string]*pubsub.Topic),
		network: dtypes.NetworkName(networkName),
	}, nil
}

func (c *PubsubClient) ConnectToLotus(lotusMultiaddr string) error {
	addr, err := peer.AddrInfoFromString(lotusMultiaddr)
	if err != nil {
		return fmt.Errorf("invalid multiaddr: %w", err)
	}

	if err := c.host.Connect(context.Background(), *addr); err != nil {
		return fmt.Errorf("failed to connect to lotus node: %w", err)
	}

	// Join topics
	blocksTopic := fmt.Sprintf(BlockTopicFormat, c.network)
	msgsTopic := fmt.Sprintf(MessageTopicFormat, c.network)

	if err := c.joinTopic(blocksTopic); err != nil {
		return fmt.Errorf("failed to join blocks topic: %w", err)
	}
	if err := c.joinTopic(msgsTopic); err != nil {
		return fmt.Errorf("failed to join messages topic: %w", err)
	}

	log.Printf("Connected to Lotus node %s and joined topics", addr.ID)
	return nil
}

func (c *PubsubClient) joinTopic(topicName string) error {
	topic, err := c.pubsub.Join(topicName)
	if err != nil {
		return fmt.Errorf("failed to join topic %s: %w", topicName, err)
	}
	c.topics[topicName] = topic

	// Subscribe to messages
	sub, err := topic.Subscribe()
	if err != nil {
		return fmt.Errorf("failed to subscribe to topic %s: %w", topicName, err)
	}

	// Handle messages in a goroutine
	go func() {
		for {
			msg, err := sub.Next(context.Background())
			if err != nil {
				log.Printf("Error receiving message on %s: %v", topicName, err)
				continue
			}

			// Handle different message types
			switch topicName {
			case fmt.Sprintf(BlockTopicFormat, c.network):
				c.handleBlockMessage(msg)
			case fmt.Sprintf(MessageTopicFormat, c.network):
				c.handleChainMessage(msg)
			}
		}
	}()

	return nil
}

func (c *PubsubClient) handleBlockMessage(msg *pubsub.Message) {
	var blk types.BlockMsg
	if err := blk.UnmarshalCBOR(bytes.NewReader(msg.Data)); err != nil {
		log.Printf("Error decoding block message: %v", err)
		return
	}
	log.Printf("Received block: %s, height: %d", blk.Header.Cid(), blk.Header.Height)
}

func (c *PubsubClient) handleChainMessage(msg *pubsub.Message) {
	var sm types.SignedMessage
	if err := sm.UnmarshalCBOR(bytes.NewReader(msg.Data)); err != nil {
		log.Printf("Error decoding chain message as SignedMessage: %v", err)
		// Try decoding as regular Message as fallback
		var m types.Message
		if err := m.UnmarshalCBOR(bytes.NewReader(msg.Data)); err != nil {
			log.Printf("Error decoding chain message as Message: %v", err)
			return
		}
		log.Printf("Received message: from=%s, to=%s, nonce=%d", m.From, m.To, m.Nonce)
		return
	}
	log.Printf("Received signed message: from=%s, to=%s, nonce=%d", sm.Message.From, sm.Message.To, sm.Message.Nonce)
}

func (c *PubsubClient) PublishBlock(block *types.BlockMsg) error {
	var buf bytes.Buffer
	if err := block.MarshalCBOR(&buf); err != nil {
		return fmt.Errorf("failed to marshal block: %w", err)
	}

	topic, ok := c.topics[fmt.Sprintf(BlockTopicFormat, c.network)]
	if !ok {
		return fmt.Errorf("not subscribed to blocks topic")
	}

	return topic.Publish(context.Background(), buf.Bytes())
}

func (c *PubsubClient) PublishMessage(msg *types.Message) error {
	msgBytes, err := msg.Serialize()
	if err != nil {
		return fmt.Errorf("failed to serialize message: %w", err)
	}

	topic, ok := c.topics[fmt.Sprintf(MessageTopicFormat, c.network)]
	if !ok {
		return fmt.Errorf("not subscribed to messages topic")
	}

	return topic.Publish(context.Background(), msgBytes)
}

func GetNetworkName(nodeAddr string, token string) (dtypes.NetworkName, error) {
	headers := http.Header{}
	if token != "" {
		headers.Add("Authorization", "Bearer "+token)
	}

	api, closer, err := client.NewFullNodeRPCV1(context.Background(), nodeAddr, headers)
	if err != nil {
		return "", fmt.Errorf("failed to create API client: %w", err)
	}
	defer closer()

	networkName, err := api.StateNetworkName(context.Background())
	if err != nil {
		return "", fmt.Errorf("failed to get network name: %w", err)
	}

	return networkName, nil
}

// FuzzConfig holds configuration for fuzzing operations
type FuzzConfig struct {
	MaxMessageSize  int
	MaxBlockHeight  int64
	FuzzInterval    time.Duration
	NumFuzzMessages int
	NumFuzzBlocks   int
}

// GenerateRandomMessage creates a random message with fuzzy data
func GenerateRandomMessage() *types.Message {
	// Generate random addresses
	to, _ := address.NewIDAddress(uint64(rand.Int63()))
	from, _ := address.NewIDAddress(uint64(rand.Int63()))

	// Generate random nonce and value
	nonce := uint64(rand.Int63())
	value := types.NewInt(uint64(rand.Int63()))

	// Generate random params (sometimes nil, sometimes random bytes)
	var params []byte
	if rand.Float32() < 0.5 {
		params = make([]byte, rand.Intn(100))
		rand.Read(params)
	}

	return &types.Message{
		Version: 0,
		To:      to,
		From:    from,
		Nonce:   nonce,
		Value:   value,
		Method:  abi.MethodNum(rand.Intn(1000)),
		Params:  params,
	}
}

// GenerateRandomBlock creates a random block with fuzzy data
func GenerateRandomBlock() *types.BlockMsg {
	// Generate random miner address
	miner, _ := address.NewIDAddress(uint64(rand.Int63()))

	// Generate random ticket and election proof
	ticket := make([]byte, 32)
	electionProof := make([]byte, 32)
	rand.Read(ticket)
	rand.Read(electionProof)

	return &types.BlockMsg{
		Header: &types.BlockHeader{
			Miner:         miner,
			Ticket:        &types.Ticket{VRFProof: ticket},
			ElectionProof: &types.ElectionProof{VRFProof: electionProof},
			Height:        abi.ChainEpoch(rand.Int63n(1000000)),
			ParentWeight:  types.NewInt(uint64(rand.Int63())),
			ParentBaseFee: types.NewInt(uint64(rand.Int63())),
		},
	}
}

func (c *PubsubClient) PublishEmptyMessage() error {
	// Try to publish empty message to both topics
	blocksTopic := fmt.Sprintf(BlockTopicFormat, c.network)
	msgsTopic := fmt.Sprintf(MessageTopicFormat, c.network)

	// Empty block message
	topic, ok := c.topics[blocksTopic]
	if ok {
		if err := topic.Publish(context.Background(), []byte{}); err != nil {
			log.Printf("Failed to publish empty block message: %v", err)
		} else {
			log.Printf("Published empty block message")
		}
	}

	// Empty chain message
	topic, ok = c.topics[msgsTopic]
	if ok {
		if err := topic.Publish(context.Background(), []byte{}); err != nil {
			log.Printf("Failed to publish empty chain message: %v", err)
		} else {
			log.Printf("Published empty chain message")
		}
	}

	// Nil message
	topic, ok = c.topics[msgsTopic]
	if ok {
		if err := topic.Publish(context.Background(), nil); err != nil {
			log.Printf("Failed to publish nil message: %v", err)
		} else {
			log.Printf("Published nil message")
		}
	}

	return nil
}

// StartFuzzing begins the fuzzing operation
func (c *PubsubClient) StartFuzzing(config FuzzConfig) {
	log.Printf("Starting fuzzing with config: %+v", config)

	// Start message fuzzing
	go func() {
		for i := 0; i < config.NumFuzzMessages; i++ {
			// 20% chance to send empty message
			if rand.Float32() < 0.2 {
				if err := c.PublishEmptyMessage(); err != nil {
					log.Printf("Failed to publish empty message: %v", err)
				}
			} else {
				msg := GenerateRandomMessage()
				if err := c.PublishMessage(msg); err != nil {
					log.Printf("Failed to publish fuzz message %d: %v", i, err)
				} else {
					log.Printf("Published fuzz message %d: from=%s, to=%s, nonce=%d", i, msg.From, msg.To, msg.Nonce)
				}
			}
			time.Sleep(config.FuzzInterval)
		}
	}()

	// Start block fuzzing
	go func() {
		for i := 0; i < config.NumFuzzBlocks; i++ {
			block := GenerateRandomBlock()
			if err := c.PublishBlock(block); err != nil {
				log.Printf("Failed to publish fuzz block %d: %v", i, err)
			} else {
				log.Printf("Published fuzz block %d: miner=%s, height=%d", i, block.Header.Miner, block.Header.Height)
			}
			time.Sleep(config.FuzzInterval)
		}
	}()

	// Sometimes send completely random data
	go func() {
		for i := 0; i < config.NumFuzzMessages; i++ {
			if rand.Float32() < 0.1 { // 10% chance of sending random data
				randomData := make([]byte, rand.Intn(config.MaxMessageSize))
				rand.Read(randomData)

				topic := c.topics[fmt.Sprintf(MessageTopicFormat, c.network)]
				if topic != nil {
					if err := topic.Publish(context.Background(), randomData); err != nil {
						log.Printf("Failed to publish random data %d: %v", i, err)
					} else {
						log.Printf("Published random data message %d of size %d bytes", i, len(randomData))
					}
				}
			}
			time.Sleep(config.FuzzInterval)
		}
	}()
}

func main() {
	// Get environment variables
	lotusAPI := os.Getenv("LOTUS_API")
	lotusToken := os.Getenv("LOTUS_TOKEN")
	lotusP2PAddr := os.Getenv("LOTUS_P2P_ADDR")

	// Validate required environment variables
	if lotusAPI == "" {
		log.Fatal("LOTUS_API environment variable not set")
	}
	if lotusP2PAddr == "" {
		log.Fatal("LOTUS_P2P_ADDR environment variable not set")
	}

	// Get network name from node
	networkName, err := GetNetworkName(lotusAPI, lotusToken)
	if err != nil {
		log.Printf("Warning: Could not verify network name from node: %v", err)
	} else {
		os.Setenv("LOTUS_NETWORK", string(networkName))
		log.Printf("Connected to Lotus network: %s", networkName)
	}

	// Create pubsub client
	client, err := NewPubsubClient()
	if err != nil {
		log.Fatal(err)
	}
	defer client.host.Close()

	// Add fuzzing configuration
	fuzzConfig := FuzzConfig{
		MaxMessageSize:  1024 * 10, // 10KB max message size
		MaxBlockHeight:  1000000,
		FuzzInterval:    time.Second * 2,
		NumFuzzMessages: 50,
		NumFuzzBlocks:   20,
	}

	// Start fuzzing after connecting
	if err := client.ConnectToLotus(lotusP2PAddr); err != nil {
		log.Fatal(err)
	}

	log.Printf("Starting fuzzing operations...")
	client.StartFuzzing(fuzzConfig)

	// Keep the program running
	select {}
}
