package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"time"

	"github.com/filecoin-project/go-address"
	cborutil "github.com/filecoin-project/go-cbor-util"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api/client"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
	"github.com/ipfs/go-cid"
	cbg "github.com/whyrusleeping/cbor-gen"
	"golang.org/x/xerrors"

	"github.com/libp2p/go-libp2p"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	pb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/protocol/ping"

	"github.com/ipld/go-ipld-prime/codec/dagcbor"
	"github.com/ipld/go-ipld-prime/node/basicnode"
	"github.com/minio/blake2b-simd"
)

const (
	GossipScoreThreshold    = -500
	PublishScoreThreshold   = -1000
	GraylistScoreThreshold  = -2500
	BlockTopicFormat        = "/fil/blocks/%s"
	MessageTopicFormat      = "/fil/msgs/%s"
	HelloProtocolID         = "/fil/hello/1.0.0"
	ChainExchangeProtocolID = "/fil/chain/xchg/0.0.1"
)

type PubsubClient struct {
	host    host.Host
	pubsub  *pubsub.PubSub
	topics  map[string]*pubsub.Topic
	network dtypes.NetworkName
}

// LocalHelloMessage is a local replication of the real hello.HelloMessage struct.
// This avoids importing the entire lotus hello package and its heavy dependencies.
type LocalHelloMessage struct {
	GenesisHash          cid.Cid
	HeaviestTipSet       []cid.Cid
	HeaviestTipSetHeight abi.ChainEpoch
	HeaviestTipSetWeight types.BigInt
}

func (t *LocalHelloMessage) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}
	if _, err := w.Write([]byte{132}); err != nil {
		return err
	}

	if err := cbg.WriteCid(w, t.GenesisHash); err != nil {
		return xerrors.Errorf("failed to write cid field t.GenesisHash: %w", err)
	}

	if len(t.HeaviestTipSet) > cbg.MaxLength {
		return xerrors.Errorf("Slice value in field t.HeaviestTipSet was too long")
	}
	if err := cbg.WriteMajorTypeHeader(w, cbg.MajArray, uint64(len(t.HeaviestTipSet))); err != nil {
		return err
	}
	for _, v := range t.HeaviestTipSet {
		if err := cbg.WriteCid(w, v); err != nil {
			return err
		}
	}

	if t.HeaviestTipSetHeight < 0 {
		return xerrors.Errorf("cannot cbor-encode negative value t.HeaviestTipSetHeight of type abi.ChainEpoch")
	}
	if err := cbg.WriteMajorTypeHeader(w, cbg.MajUnsignedInt, uint64(t.HeaviestTipSetHeight)); err != nil {
		return err
	}

	if err := t.HeaviestTipSetWeight.MarshalCBOR(w); err != nil {
		return err
	}
	return nil
}

func (t *LocalHelloMessage) UnmarshalCBOR(r io.Reader) error {
	br := cbg.GetPeeker(r)
	scratch := make([]byte, 8)
	maj, extra, err := cbg.CborReadHeaderBuf(br, scratch)
	if err != nil {
		return err
	}
	if maj != cbg.MajArray {
		return fmt.Errorf("cbor input should be of type array")
	}
	if extra != 4 {
		return fmt.Errorf("cbor input had wrong number of fields")
	}

	// GenesisHash (cid.Cid)
	{
		c, err := cbg.ReadCid(br)
		if err != nil {
			return xerrors.Errorf("failed to read cid field GenesisHash: %w", err)
		}
		t.GenesisHash = c
	}

	// HeaviestTipSet ([]cid.Cid)
	{
		maj, extra, err = cbg.CborReadHeaderBuf(br, scratch)
		if err != nil {
			return err
		}
		if maj != cbg.MajArray {
			return fmt.Errorf("expected cbor array")
		}
		if extra > cbg.MaxLength {
			return fmt.Errorf("t.HeaviestTipSet: array too large (%d)", extra)
		}
		if extra > 0 {
			t.HeaviestTipSet = make([]cid.Cid, extra)
		}
		for i := 0; i < int(extra); i++ {
			c, err := cbg.ReadCid(br)
			if err != nil {
				return xerrors.Errorf("failed to read cid field t.HeaviestTipSet: %w", err)
			}
			t.HeaviestTipSet[i] = c
		}
	}

	// HeaviestTipSetHeight (abi.ChainEpoch)
	{
		maj, extra, err = cbg.CborReadHeaderBuf(br, scratch)
		if err != nil {
			return err
		}
		if maj != cbg.MajUnsignedInt {
			return fmt.Errorf("wrong type for abi.ChainEpoch field")
		}
		t.HeaviestTipSetHeight = abi.ChainEpoch(extra)
	}

	// HeaviestTipSetWeight (types.BigInt)
	{
		if err := t.HeaviestTipSetWeight.UnmarshalCBOR(br); err != nil {
			return xerrors.Errorf("unmarshaling t.HeaviestTipSetWeight: %w", err)
		}
	}
	return nil
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
	MaxMessageSize int
	MaxBlockHeight int64
	FuzzInterval   time.Duration
	StreamTimeout  time.Duration
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

// StartFuzzing begins the fuzzing operation for pubsub and hello protocol.
func (c *PubsubClient) StartFuzzing(config FuzzConfig, targetPeer string) {
	log.Printf("Starting fuzzing with config: %+v", config)

	// Start message fuzzing
	/*
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
	*/

	// Start block fuzzing
	/*
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
	*/

	// Start hello protocol fuzzing
	go func() {
		for {
			if err := c.FuzzHelloMessage(context.Background(), targetPeer, config); err != nil {
				log.Printf("[ERROR] Hello fuzzing iteration failed: %v", err)
			}
			time.Sleep(config.FuzzInterval)
		}
	}()

	// Start ChainExchange fuzzing
	go func() {
		for {
			if err := c.FuzzChainExchange(context.Background(), targetPeer, config); err != nil {
				log.Printf("[ERROR] ChainExchange fuzzing iteration failed: %v", err)
			}
			time.Sleep(config.FuzzInterval)
		}
	}()
}

// SendHelloMessage connects to a peer, sends a hello message, and logs the response.
func (c *PubsubClient) SendHelloMessage(ctx context.Context, lotusAPI, lotusToken, targetPeer string) error {
	log.Printf("Attempting to send Hello message to %s", targetPeer)

	// 1. Get necessary chain info from the node we are connected to via RPC
	headers := http.Header{}
	if lotusToken != "" {
		headers.Add("Authorization", "Bearer "+lotusToken)
	}
	api, closer, err := client.NewFullNodeRPCV1(ctx, lotusAPI, headers)
	if err != nil {
		return fmt.Errorf("failed to create API client: %w", err)
	}
	defer closer()

	genesis, err := api.ChainGetGenesis(ctx)
	if err != nil {
		return fmt.Errorf("failed to get genesis: %w", err)
	}

	head, err := api.ChainHead(ctx)
	if err != nil {
		return fmt.Errorf("failed to get chain head: %w", err)
	}

	// 2. Construct the HelloMessage
	msg := LocalHelloMessage{
		GenesisHash:          genesis.Cids()[0],
		HeaviestTipSet:       head.Cids(),
		HeaviestTipSetHeight: head.Height(),
		HeaviestTipSetWeight: head.MinTicketBlock().ParentWeight,
	}
	log.Printf("Constructed Hello message with head at height %d", msg.HeaviestTipSetHeight)

	// 3. Open a stream to the target peer
	addr, err := peer.AddrInfoFromString(targetPeer)
	log.Printf("hello fuzz: target peer: %s", targetPeer)
	if err != nil {
		return fmt.Errorf("invalid multiaddr: %w", err)
	}

	s, err := c.host.NewStream(ctx, addr.ID, HelloProtocolID)
	if err != nil {
		return fmt.Errorf("failed to open new stream: %w", err)
	}
	defer s.Close()

	// 4. Send the message
	if err := cborutil.WriteCborRPC(s, &msg); err != nil {
		return fmt.Errorf("failed to write hello message: %w", err)
	}

	log.Println("Successfully sent Hello message.")

	// 5. Read the response
	var respMsg LocalHelloMessage
	if err := cborutil.ReadCborRPC(s, &respMsg); err != nil {
		log.Printf("Failed to read response to hello message: %v (this may be expected if peer just closes)", err)
	} else {
		log.Printf("Received Hello response with head at height %d", respMsg.HeaviestTipSetHeight)
	}

	return nil
}

// FuzzHelloMessage sends arbitrary data to the hello protocol stream to test robustness.
func (c *PubsubClient) FuzzHelloMessage(ctx context.Context, targetPeer string, config FuzzConfig) error {
	log.Println("--- New Hello Fuzz Iteration ---")

	addr, err := peer.AddrInfoFromString(targetPeer)
	log.Printf("hello fuzz: target peer: %s", targetPeer)
	if err != nil {
		return fmt.Errorf("invalid multiaddr: %w", err)
	}

	// Explicitly connect and ping the peer to ensure a healthy connection.
	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := c.host.Connect(connectCtx, *addr); err != nil {
		log.Printf("hello fuzz: failed to connect to peer %s: %v. This may be expected.", addr.ID, err)
		return nil // Don't treat as a fatal error for the fuzzer, just skip this iteration.
	}

	// Ping the peer to verify the connection is alive.
	pingService := ping.NewPingService(c.host)
	pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
	defer pingCancel()
	ch := pingService.Ping(pingCtx, addr.ID)
	result := <-ch
	if result.Error != nil {
		log.Printf("hello fuzz: failed to ping peer %s: %v. Skipping iteration.", addr.ID, result.Error)
		return nil
	}
	log.Printf("hello fuzz: successfully pinged peer %s in %s", addr.ID, result.RTT)

	s, err := c.host.NewStream(ctx, addr.ID, HelloProtocolID)
	if err != nil {
		log.Printf("failed to open new stream for hello: %v. This may be expected.", err)
		return nil // Don't treat as a fatal error for the fuzzer.
	}
	defer s.Close()

	// Set a deadline for the operation
	s.SetDeadline(time.Now().Add(config.StreamTimeout))
	defer s.Close()
	defer s.SetDeadline(time.Time{})

	// Send a large random payload
	payload := make([]byte, 1<<20) // 1MB
	_, _ = rand.Read(payload)

	// Write the fuzzed data
	n, err := s.Write(payload)
	if err != nil {
		return fmt.Errorf("failed to write fuzzed data: %w", err)
	}

	log.Printf("Successfully sent %d bytes.", n)
	return nil
}

// FuzzChainExchange sends arbitrary data to the chainexchange protocol stream to test robustness.
func (c *PubsubClient) FuzzChainExchange(ctx context.Context, targetPeer string, config FuzzConfig) error {
	log.Println("--- New ChainExchange Fuzz Iteration ---")

	addr, err := peer.AddrInfoFromString(targetPeer)
	log.Printf("chainexchange fuzz: target peer: %s", targetPeer)
	if err != nil {
		return fmt.Errorf("invalid multiaddr for chainexchange: %w", err)
	}

	// Explicitly connect and ping the peer to ensure a healthy connection.
	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := c.host.Connect(connectCtx, *addr); err != nil {
		log.Printf("chainexchange fuzz: failed to connect to peer %s: %v. This may be expected.", addr.ID, err)
		return nil // Don't treat as a fatal error for the fuzzer, just skip this iteration.
	}

	// Ping the peer to verify the connection is alive.
	pingService := ping.NewPingService(c.host)
	pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
	defer pingCancel()
	ch := pingService.Ping(pingCtx, addr.ID)
	result := <-ch
	if result.Error != nil {
		log.Printf("chainexchange fuzz: failed to ping peer %s: %v. Skipping iteration.", addr.ID, result.Error)
		return nil
	}
	log.Printf("chainexchange fuzz: successfully pinged peer %s", addr.ID)

	s, err := c.host.NewStream(ctx, addr.ID, ChainExchangeProtocolID)
	if err != nil {
		// It's possible the peer doesn't support the protocol, which is not a critical failure.
		log.Printf("Could not open stream for ChainExchange, peer may not support it: %v", err)
		return nil
	}
	defer s.Close()

	// Set a deadline for writing and reading to avoid blocking indefinitely.
	s.SetDeadline(time.Now().Add(config.StreamTimeout))
	defer s.Close()
	defer s.SetDeadline(time.Time{}) // Clear the deadline

	var payload []byte
	fuzzType := rand.Intn(7) // Expanded to 7 fuzzing strategies

	switch fuzzType {
	case 0:
		// Strategy 1: Large Garbage (1MB of random bytes)
		size := 1 * 1024 * 1024
		payload = make([]byte, size)
		_, _ = rand.Read(payload)
		log.Printf("ChainExchange Fuzz: Sending %d bytes of large random garbage", len(payload))

	case 1:
		// Strategy 2: Structurally invalid CBOR (map instead of array for the request)
		nb := basicnode.Prototype.Any.NewBuilder()
		ma, _ := nb.BeginMap(-1)
		ma.AssembleKey().AssignString("Head")
		ma.AssembleValue().AssignInt(123)
		ma.Finish()
		node := nb.Build()

		var buf bytes.Buffer
		err = dagcbor.Encode(node, &buf)
		if err != nil {
			return fmt.Errorf("failed to encode invalid cbor for chainexchange: %w", err)
		}
		payload = buf.Bytes()
		log.Printf("ChainExchange Fuzz: Sending %d bytes of structurally incorrect CBOR (map)", len(payload))

	case 2:
		// Strategy 3: Logically incorrect - huge length
		// Represents: [[], 18446744073709551615, 1]  (empty head, max uint64 length, option for headers)
		payload = []byte{
			0x83,                                                 // array(3)
			0x80,                                                 // array(0)
			0x1b, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, // uint64_max
			0x01, // 1 (Headers option)
		}
		log.Printf("ChainExchange Fuzz: Sending %d bytes of huge length request", len(payload))

	case 3:
		// Strategy 4: Empty payload
		payload = []byte{}
		log.Printf("ChainExchange Fuzz: Sending empty payload")

	case 4:
		// Strategy 5: Invalid Options field.
		// Represents: [[], 10, 1<<5]  (empty head, length 10, unknown option)
		payload = []byte{
			0x83, // array(3)
			0x80, // array(0)
			0x0a, // 10
			0x20, // 32 (1 << 5)
		}
		log.Printf("ChainExchange Fuzz: Sending request with invalid options flag")

	case 5:
		// Strategy 6: Resource exhaustion - large Head array
		// Represents: [[cid1, cid2, ...], 10, 1]
		nb := basicnode.Prototype.Any.NewBuilder()
		la, _ := nb.BeginList(2000) // Large number of CIDs
		for i := 0; i < 2000; i++ {
			// Create a garbage CID-like structure
			fakeCidBytes := make([]byte, 36)
			rand.Read(fakeCidBytes)
			fakeCid, _ := cid.Cast(fakeCidBytes)
			la.AssembleValue().AssignBytes(fakeCid.Bytes())
		}
		la.Finish()
		node := nb.Build()

		var buf bytes.Buffer
		err = cborutil.WriteCborRPC(&buf, node)
		if err != nil {
			return fmt.Errorf("failed to encode large head array: %w", err)
		}
		payload = buf.Bytes()
		log.Printf("ChainExchange Fuzz: Sending request with very large Head array (%d bytes)", len(payload))

	default:
		// Strategy 7: Simple Malformed CBOR
		payload = []byte{0xA1, 0x68, 'g', 'a', 'r', 'b', 'a', 'g', 'e'}
		log.Printf("ChainExchange Fuzz: Sending %d bytes of simple malformed CBOR", len(payload))
	}

	// Set a deadline for the write to prevent hanging
	s.SetWriteDeadline(time.Now().Add(10 * time.Second))
	n, err := s.Write(payload)
	if err != nil {
		// Log as info as the connection may be justifiably closed by the remote peer
		log.Printf("Info: failed to write fuzzed data to ChainExchange: %v", err)
		return nil
	}

	log.Printf("Successfully sent %d bytes to ChainExchange.", n)

	// Try to read a response, but don't block forever.
	// The remote peer might not send a response for malformed data.
	response := make([]byte, 1024)
	n, err = s.Read(response)
	if err != nil && err != io.EOF {
		log.Printf("Info: reading response from ChainExchange fuzz: %v", err)
		return nil
	}
	if n > 0 {
		log.Printf("Received %d bytes of response from ChainExchange fuzz", n)
	} else {
		log.Printf("Received 0 bytes of response from ChainExchange fuzz")
	}

	return nil
}

func main() {
	// Get environment variables
	lotusAPI := os.Getenv("LOTUS_API")
	lotusToken := os.Getenv("LOTUS_TOKEN")
	lotusP2PAddr := os.Getenv("LOTUS_P2P_ADDR")

	// Validate required environment variables
	if lotusP2PAddr == "" {
		log.Fatal("LOTUS_P2P_ADDR environment variable not set")
	}

	// Get network name from node if API is provided
	if lotusAPI != "" {
		networkName, err := GetNetworkName(lotusAPI, lotusToken)
		if err != nil {
			log.Printf("Warning: Could not verify network name from node: %v", err)
		} else {
			os.Setenv("LOTUS_NETWORK", string(networkName))
			log.Printf("Set network to: %s", networkName)
		}
	}

	// Create pubsub client
	client, err := NewPubsubClient()
	if err != nil {
		log.Fatal(err)
	}
	defer client.host.Close()

	// Connect to the peer to join pubsub topics
	if err := client.ConnectToLotus(lotusP2PAddr); err != nil {
		log.Fatalf("Failed to connect to lotus for pubsub: %v", err)
	}

	// Define the fuzzing configuration
	fuzzConfig := FuzzConfig{
		MaxMessageSize: 1024 * 10,
		MaxBlockHeight: 1000000,
		FuzzInterval:   time.Second * 3,
		StreamTimeout:  30 * time.Second,
	}

	// Start the unified fuzzer
	log.Printf("Starting all fuzzing operations against %s", lotusP2PAddr)
	client.StartFuzzing(fuzzConfig, lotusP2PAddr)

	// Keep the main function running indefinitely
	select {}
}
