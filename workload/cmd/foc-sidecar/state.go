package main

import (
	"math/big"
	"sync"
)

// SidecarState tracks in-memory state derived from on-chain events.
type SidecarState struct {
	mu sync.RWMutex

	Datasets      map[uint64]*DatasetInfo // dataSetId -> info
	Rails         map[uint64]*RailInfo    // railId -> info
	RailToDataset map[uint64]uint64       // railId -> dataSetId
	TrackedPayers [][]byte                // unique payer addresses seen
}

// DatasetInfo holds state for a tracked dataset.
type DatasetInfo struct {
	DataSetID       uint64
	ProviderID      uint64
	PDPRailID       uint64
	FilPayRailID    uint64
	Payer           []byte
	ServiceProvider []byte
	Payee           []byte
}

// RailInfo holds state for a tracked payment rail.
type RailInfo struct {
	RailID uint64
	Token  []byte
	From   []byte
	To     []byte
}

// NewSidecarState creates an initialized SidecarState.
func NewSidecarState() *SidecarState {
	return &SidecarState{
		Datasets:      make(map[uint64]*DatasetInfo),
		Rails:         make(map[uint64]*RailInfo),
		RailToDataset: make(map[uint64]uint64),
	}
}

// AddDataset records a newly created dataset from a DataSetCreated event.
func (s *SidecarState) AddDataset(ev DataSetCreatedEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	dsID := ev.DataSetID.Uint64()
	s.Datasets[dsID] = &DatasetInfo{
		DataSetID:       dsID,
		ProviderID:      ev.ProviderID.Uint64(),
		PDPRailID:       ev.PDPRailID.Uint64(),
		FilPayRailID:    ev.FilPayRailID.Uint64(),
		Payer:           ev.Payer,
		ServiceProvider: ev.ServiceProvider,
		Payee:           ev.Payee,
	}

	s.RailToDataset[ev.PDPRailID.Uint64()] = dsID

	// Track unique payers
	found := false
	for _, p := range s.TrackedPayers {
		if bytesEqual(p, ev.Payer) {
			found = true
			break
		}
	}
	if !found {
		s.TrackedPayers = append(s.TrackedPayers, ev.Payer)
	}
}

// AddRail records a newly created payment rail.
func (s *SidecarState) AddRail(ev RailCreatedEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rID := ev.RailID.Uint64()
	s.Rails[rID] = &RailInfo{
		RailID: rID,
		Token:  ev.Token,
		From:   ev.From,
		To:     ev.To,
	}
}

// GetDatasets returns a snapshot of all tracked datasets.
func (s *SidecarState) GetDatasets() []*DatasetInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*DatasetInfo, 0, len(s.Datasets))
	for _, d := range s.Datasets {
		result = append(result, d)
	}
	return result
}

// GetTrackedPayers returns a snapshot of tracked payer addresses.
func (s *SidecarState) GetTrackedPayers() [][]byte {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([][]byte, len(s.TrackedPayers))
	copy(out, s.TrackedPayers)
	return out
}

// GetRailToDataset returns the expected dataSetId for a given railId.
func (s *SidecarState) GetRailToDataset(railID uint64) (uint64, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	dsID, ok := s.RailToDataset[railID]
	return dsID, ok
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// bigIntFromUint64 is a convenience wrapper.
func bigIntFromUint64(n uint64) *big.Int {
	return new(big.Int).SetUint64(n)
}
