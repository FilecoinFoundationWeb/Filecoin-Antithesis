package test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"

	"github.com/filecoin-project/go-f3/merkle"
	"github.com/filecoin-project/go-state-types/big"
	big_type "github.com/filecoin-project/go-state-types/big"
	"github.com/multiformats/go-multihash"
	cbg "github.com/whyrusleeping/cbor-gen"

	"github.com/filecoin-project/go-bitfield"
	"github.com/ipfs/go-cid"
)

// ErrNoPower is returned by the MEssageBuilder if the specified participant has no power.
var ErrNoPower = errors.New("no power")

type NetworkName string

type Phase uint8

type SupplementalData struct {
	// Merkle-tree of instance-specific commitments. Currently empty but this will eventually
	// include things like snark-friendly power-table commitments.
	Commitments [32]byte
	// The DagCBOR-blake2b256 CID of the power table used to validate the next instance, taking
	// lookback into account.
	PowerTable cid.Cid // []PowerEntry
}

type ECChain []TipSet

// TipSet represents a single EC tipset.
type TipSet struct {
	// The EC epoch (strictly increasing).
	Epoch int64
	// The tipset's key (canonically ordered concatenated block-header CIDs).
	Key TipSetKey `cborgen:"maxlen=760"` // 20 * 38B
	// Blake2b256-32 CID of the CBOR-encoded power table.
	PowerTable cid.Cid
	// Keccak256 root hash of the commitments merkle tree.
	Commitments [32]byte
}

// TipSetKey is the canonically ordered concatenation of the block CIDs in a tipset.
type TipSetKey = []byte

// Fields of the message that make up the signature payload.
type Payload struct {
	// GossiPBFT instance (epoch) number.
	Instance uint64
	// GossiPBFT round number.
	Round uint64
	// GossiPBFT phase name.
	Phase Phase
	// The common data.
	SupplementalData SupplementalData
	// The value agreed-upon in a single instance.
	Value ECChain
}

type Justification struct {
	// Vote is the payload that is signed by the signature
	Vote Payload
	// Indexes in the base power table of the signers (bitset)
	Signers bitfield.BitField
	// BLS aggregate signature of signers
	Signature []byte `cborgen:"maxlen=96"`
}

type SigningMarshaler interface {
	// MarshalPayloadForSigning marshals the given payload into the bytes that should be signed.
	// This should usually call `Payload.MarshalForSigning(NetworkName)` except when testing as
	// that method is slow (computes a merkle tree that's necessary for testing).
	// Implementations must be safe for concurrent use.
	MarshalPayloadForSigning(NetworkName, *Payload) []byte
}

type MessageBuilder struct {
	NetworkName      NetworkName
	PowerTable       powerTableAccessor
	Payload          Payload
	BeaconForTicket  []byte
	Justification    *Justification
	SigningMarshaler SigningMarshaler
}

type ActorID uint64

type PubKey []byte

type powerTableAccessor interface {
	Get(ActorID) (int64, PubKey)
}

// SignatureBuilder's fields are exposed to facilitate JSON encoding
type SignatureBuilder struct {
	NetworkName NetworkName

	ParticipantID ActorID
	Payload       Payload
	Justification *Justification
	PubKey        PubKey
	PayloadToSign []byte
	VRFToSign     []byte
}

type Signer interface {
	// Signs a message with the secret key corresponding to a public key.
	Sign(ctx context.Context, sender PubKey, msg []byte) ([]byte, error)
}

const DomainSeparationTagVRF = "VRF"

type Ticket []byte

type GMessage struct {
	// ID of the sender/signer of this message (a miner actor ID).
	Sender ActorID
	// Vote is the payload that is signed by the signature
	Vote Payload
	// Signature by the sender's public key over Instance || Round || Phase || Value.
	Signature []byte `cborgen:"maxlen=96"`
	// VRF ticket for CONVERGE messages (otherwise empty byte array).
	Ticket Ticket `cborgen:"maxlen=96"`
	// Justification for this message (some messages must be justified by a strong quorum of messages from some previous phase).
	Justification *Justification
}

func vrfSerializeSigInput(beacon []byte, instance uint64, round uint64, networkName NetworkName) []byte {
	var buf bytes.Buffer

	buf.WriteString(DomainSeparationTagVRF)
	buf.WriteString(":")
	buf.WriteString(string(networkName))
	buf.WriteString(":")
	buf.Write(beacon)
	buf.WriteString(":")
	_ = binary.Write(&buf, binary.BigEndian, instance)
	_ = binary.Write(&buf, binary.BigEndian, round)

	return buf.Bytes()
}

// Build uses the builder and a signer interface to build GMessage
// It is a shortcut for when separated flow is not required
func (mt *MessageBuilder) Build(ctx context.Context, signer Signer, id ActorID) (*GMessage, error) {
	st, err := mt.PrepareSigningInputs(id)
	if err != nil {
		return nil, fmt.Errorf("preparing signing inputs: %w", err)
	}

	payloadSig, vrf, err := st.Sign(ctx, signer)
	if err != nil {
		return nil, fmt.Errorf("signing message builder: %w", err)
	}

	return st.Build(payloadSig, vrf), nil
}

func (mb *MessageBuilder) PrepareSigningInputs(id ActorID) (*SignatureBuilder, error) {
	effectivePower, pubKey := mb.PowerTable.Get(id)
	if pubKey == nil || effectivePower == 0 {
		return nil, fmt.Errorf("could not find pubkey for actor %d: %w", id, ErrNoPower)
	}
	sb := SignatureBuilder{
		ParticipantID: id,
		NetworkName:   mb.NetworkName,
		Payload:       mb.Payload,
		Justification: mb.Justification,

		PubKey: pubKey,
	}

	sb.PayloadToSign = mb.SigningMarshaler.MarshalPayloadForSigning(mb.NetworkName, &mb.Payload)
	if mb.BeaconForTicket != nil {
		sb.VRFToSign = vrfSerializeSigInput(mb.BeaconForTicket, mb.Payload.Instance, mb.Payload.Round, mb.NetworkName)
	}
	return &sb, nil
}

// Sign creates the signed payload from the signature builder and returns the payload
// and VRF signatures. These signatures can be used independent from the builder.
func (st *SignatureBuilder) Sign(ctx context.Context, signer Signer) ([]byte, []byte, error) {
	payloadSignature, err := signer.Sign(ctx, st.PubKey, st.PayloadToSign)
	if err != nil {
		return nil, nil, fmt.Errorf("signing payload: %w", err)
	}
	var vrf []byte
	if st.VRFToSign != nil {
		vrf, err = signer.Sign(ctx, st.PubKey, st.VRFToSign)
		if err != nil {
			return nil, nil, fmt.Errorf("signing vrf: %w", err)
		}
	}
	return payloadSignature, vrf, nil
}

// Build takes the template and signatures and builds GMessage out of them
func (st *SignatureBuilder) Build(payloadSignature []byte, vrf []byte) *GMessage {
	return &GMessage{
		Sender:        st.ParticipantID,
		Vote:          st.Payload,
		Signature:     payloadSignature,
		Ticket:        vrf,
		Justification: st.Justification,
	}
}

// TESTING

type StoragePower = big_type.Int

type PowerEntry struct {
	ID     ActorID
	Power  StoragePower
	PubKey PubKey `cborgen:"maxlen=48"`
}

type PowerEntries []PowerEntry

type PowerTable struct {
	Entries     PowerEntries // Slice to maintain the order. Meant to be maintained in order in order by (Power descending, ID ascending)
	ScaledPower []int64
	Lookup      map[ActorID]int // Maps ActorID to the index of the associated entry in Entries
	Total       StoragePower
	ScaledTotal int64
}

func NewStoragePower(value int64) StoragePower {
	return big_type.NewInt(value)
}

func NewPowerTable() *PowerTable {
	return &PowerTable{
		Lookup: make(map[ActorID]int),
		Total:  NewStoragePower(0),
	}
}

func (p *PowerTable) Has(id ActorID) bool {
	_, found := p.Lookup[id]
	return found
}

func scalePower(power, total StoragePower) (int64, error) {
	const maxPower = 0xffff
	if total.LessThan(power) {
		return 0, fmt.Errorf("total power %d is less than the power of a single participant %d", total, power)
	}
	scaled := big_type.NewInt(maxPower)
	scaled = big_type.Mul(scaled, power)
	scaled = big_type.Div(scaled, total)
	return scaled.Int64(), nil
}

func (p *PowerTable) rescale() error {
	p.ScaledTotal = 0
	for i := range p.Entries {
		scaled, err := scalePower(p.Entries[i].Power, p.Total)
		if err != nil {
			return err
		}
		p.ScaledPower[i] = scaled
		p.ScaledTotal += scaled
	}
	return nil
}

func (p *PowerTable) Len() int {
	return p.Entries.Len()
}

func (p PowerEntries) Len() int {
	return len(p)
}

func (p PowerEntries) Less(i, j int) bool {
	one, other := p[i], p[j]
	switch cmp := big.Cmp(one.Power, other.Power); {
	case cmp > 0:
		return true
	case cmp == 0:
		return one.ID < other.ID
	default:
		return false
	}
}

func (p *PowerTable) Less(i, j int) bool {
	return p.Entries.Less(i, j)
}

func (p *PowerTable) Swap(i, j int) {
	p.Entries.Swap(i, j)
	p.ScaledPower[i], p.ScaledPower[j] = p.ScaledPower[j], p.ScaledPower[i]
	p.Lookup[p.Entries[i].ID], p.Lookup[p.Entries[j].ID] = i, j
}

func (p PowerEntries) Swap(i, j int) {
	p[i], p[j] = p[j], p[i]
}

func (p *PowerTable) Add(entries ...PowerEntry) error {
	for _, entry := range entries {
		switch {
		case len(entry.PubKey) == 0:
			return fmt.Errorf("unspecified public key for actor ID: %d", entry.ID)
		case p.Has(entry.ID):
			return fmt.Errorf("power entry already exists for actor ID: %d", entry.ID)
		case entry.Power.Sign() <= 0:
			return fmt.Errorf("zero power for actor ID: %d", entry.ID)
		default:
			p.Total = big_type.Add(p.Total, entry.Power)
			p.Entries = append(p.Entries, entry)
			p.ScaledPower = append(p.ScaledPower, 0)
			p.Lookup[entry.ID] = len(p.Entries) - 1
		}
	}
	sort.Sort(p)
	return p.rescale()
}

func (p *PowerTable) Get(id ActorID) (int64, PubKey) {
	if index, ok := p.Lookup[id]; ok {
		key := p.Entries[index].PubKey
		scaledPower := p.ScaledPower[index]
		return scaledPower, key
	}
	return 0, nil
}

type testSigningMarshaler struct{}

var signingMarshaler SigningMarshaler = testSigningMarshaler{}

var CidPrefix = cid.Prefix{
	Version:  1,
	Codec:    cid.DagCBOR,
	MhType:   multihash.BLAKE2B_MIN + 31,
	MhLength: 32,
}

func MakeCid(data []byte) cid.Cid {
	k, err := CidPrefix.Sum(data)
	if err != nil {
		panic(err)
	}
	return k
}

func (ts *TipSet) MarshalForSigning() []byte {
	var buf bytes.Buffer
	buf.Grow(len(ts.Key) + 4) // slight over-estimation
	_ = cbg.WriteByteArray(&buf, ts.Key)
	tsCid := MakeCid(buf.Bytes())
	buf.Reset()
	buf.Grow(tsCid.ByteLen() + ts.PowerTable.ByteLen() + 32 + 8)
	// epoch || commitments || tipset || powertable
	_ = binary.Write(&buf, binary.BigEndian, ts.Epoch)
	_, _ = buf.Write(ts.Commitments[:])
	_, _ = buf.Write(tsCid.Bytes())
	_, _ = buf.Write(ts.PowerTable.Bytes())
	return buf.Bytes()
}

const DomainSeparationTag = "GPBFT"

func (p *Payload) MarshalForSigning(nn NetworkName) []byte {
	values := make([][]byte, len(p.Value))
	for i := range p.Value {
		values[i] = p.Value[i].MarshalForSigning()
	}
	root := merkle.Tree(values)

	var buf bytes.Buffer
	buf.WriteString(DomainSeparationTag)
	buf.WriteString(":")
	buf.WriteString(string(nn))
	buf.WriteString(":")

	_ = binary.Write(&buf, binary.BigEndian, p.Phase)
	_ = binary.Write(&buf, binary.BigEndian, p.Round)
	_ = binary.Write(&buf, binary.BigEndian, p.Instance)
	_, _ = buf.Write(p.SupplementalData.Commitments[:])
	_, _ = buf.Write(root[:])
	_, _ = buf.Write(p.SupplementalData.PowerTable.Bytes())
	return buf.Bytes()
}

func (testSigningMarshaler) MarshalPayloadForSigning(nn NetworkName, p *Payload) []byte {
	return p.MarshalForSigning(nn)
}
