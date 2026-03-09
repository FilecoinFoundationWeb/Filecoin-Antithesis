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
	Payer           []byte
	ServiceProvider []byte
	Payee           []byte
	Deleted         bool

	// Proving lifecycle tracking
	LastSeenChallengeEpoch uint64 // last observed getNextChallengeEpoch value
	LastSeenProvenEpoch    uint64 // last observed getDataSetLastProvenEpoch value
	ChallengeEpochStale    int    // consecutive polls where challenge epoch didn't advance
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

// GetDatasets returns a deep-copy snapshot of all tracked datasets.
func (s *SidecarState) GetDatasets() []DatasetInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]DatasetInfo, 0, len(s.Datasets))
	for _, d := range s.Datasets {
		result = append(result, *d)
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

// UpdateProvingState updates the proving lifecycle tracking for a dataset.
// Returns (challengeAdvanced, provenAdvanced) indicating whether each metric changed.
func (s *SidecarState) UpdateProvingState(dataSetID uint64, challengeEpoch, provenEpoch uint64) (bool, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ds, ok := s.Datasets[dataSetID]
	if !ok || ds.Deleted {
		return false, false
	}

	challengeAdvanced := challengeEpoch != ds.LastSeenChallengeEpoch && challengeEpoch > 0
	provenAdvanced := provenEpoch != ds.LastSeenProvenEpoch && provenEpoch > 0

	if challengeAdvanced {
		ds.LastSeenChallengeEpoch = challengeEpoch
		ds.ChallengeEpochStale = 0
	} else if ds.LastSeenChallengeEpoch > 0 {
		ds.ChallengeEpochStale++
	}

	if provenAdvanced {
		ds.LastSeenProvenEpoch = provenEpoch
	}

	return challengeAdvanced, provenAdvanced
}

// MarkDatasetDeleted marks a dataset as deleted by its on-chain ID.
func (s *SidecarState) MarkDatasetDeleted(dataSetID uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ds, ok := s.Datasets[dataSetID]; ok {
		ds.Deleted = true
	}
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
