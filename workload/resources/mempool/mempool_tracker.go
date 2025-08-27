package mempool

import (
	"context"
	"log"
	"math"
	"sync"
	"time"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
)

// MempoolStats tracks mempool statistics over time
type MempoolStats struct {
	mu sync.RWMutex

	// Raw data points
	sizes      []int64
	timestamps []time.Time

	// Computed statistics
	totalSize   int64
	count       int64
	averageSize float64
	minSize     int64
	maxSize     int64
	lastUpdate  time.Time
}

// NewMempoolStats creates a new mempool statistics tracker
func NewMempoolStats() *MempoolStats {
	return &MempoolStats{
		sizes:      make([]int64, 0),
		timestamps: make([]time.Time, 0),
		minSize:    math.MaxInt64,
		maxSize:    math.MinInt64,
	}
}

// AddSize adds a new mempool size measurement
func (m *MempoolStats) AddSize(size int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	m.sizes = append(m.sizes, size)
	m.timestamps = append(m.timestamps, now)

	m.totalSize += size
	m.count++
	m.averageSize = float64(m.totalSize) / float64(m.count)

	if size < m.minSize {
		m.minSize = size
	}
	if size > m.maxSize {
		m.maxSize = size
	}

	m.lastUpdate = now
}

// GetStats returns current mempool statistics
func (m *MempoolStats) GetStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return map[string]interface{}{
		"total_size":   m.totalSize,
		"count":        m.count,
		"average_size": m.averageSize,
		"min_size":     m.minSize,
		"max_size":     m.maxSize,
		"last_update":  m.lastUpdate,
		"data_points":  len(m.sizes),
	}
}

// GetAverageSize returns the current average mempool size
func (m *MempoolStats) GetAverageSize() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.averageSize
}

// GetDataPoints returns all recorded data points
func (m *MempoolStats) GetDataPoints() ([]int64, []time.Time) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sizes := make([]int64, len(m.sizes))
	timestamps := make([]time.Time, len(m.timestamps))
	copy(sizes, m.sizes)
	copy(timestamps, m.timestamps)

	return sizes, timestamps
}

// MempoolTracker handles mempool monitoring with backoff
type MempoolTracker struct {
	stats      *MempoolStats
	api        api.FullNode
	interval   time.Duration
	maxRetries int
	backoff    time.Duration
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
}

// NewMempoolTracker creates a new mempool tracker
func NewMempoolTracker(api api.FullNode, interval time.Duration) *MempoolTracker {
	ctx, cancel := context.WithCancel(context.Background())

	return &MempoolTracker{
		stats:      NewMempoolStats(),
		api:        api,
		interval:   interval,
		maxRetries: 5,
		backoff:    2 * time.Second,
		ctx:        ctx,
		cancel:     cancel,
	}
}

// Start begins tracking mempool size
func (m *MempoolTracker) Start() {
	log.Printf("[INFO] Starting mempool tracker with interval: %v", m.interval)

	m.wg.Add(1)
	go m.trackLoop()
}

// Stop stops the mempool tracker
func (m *MempoolTracker) Stop() {
	log.Printf("[INFO] Stopping mempool tracker...")
	m.cancel()
	m.wg.Wait()
	log.Printf("[INFO] Mempool tracker stopped")
}

// trackLoop is the main tracking loop
func (m *MempoolTracker) trackLoop() {
	defer m.wg.Done()

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.measureMempoolSize()
		}
	}
}

// measureMempoolSize measures current mempool size with retry logic
func (m *MempoolTracker) measureMempoolSize() {
	var size int64
	var err error

	// Retry with exponential backoff
	for attempt := 0; attempt < m.maxRetries; attempt++ {
		size, err = m.getMempoolSize()
		if err == nil {
			break
		}

		log.Printf("[WARN] Failed to get mempool size (attempt %d/%d): %v",
			attempt+1, m.maxRetries, err)

		if attempt < m.maxRetries-1 {
			backoff := time.Duration(math.Pow(2, float64(attempt))) * m.backoff
			log.Printf("[INFO] Retrying in %v...", backoff)

			select {
			case <-m.ctx.Done():
				return
			case <-time.After(backoff):
				continue
			}
		}
	}

	if err != nil {
		log.Printf("[ERROR] Failed to get mempool size after %d attempts: %v", m.maxRetries, err)
		return
	}

	m.stats.AddSize(size)
	log.Printf("[INFO] Mempool size: %d, Average: %.2f", size, m.stats.GetAverageSize())
}

// getMempoolSize gets the current mempool size from the node using MpoolPending
func (m *MempoolTracker) getMempoolSize() (int64, error) {
	ctx, cancel := context.WithTimeout(m.ctx, 10*time.Second)
	defer cancel()

	// Get pending messages from mempool using MpoolPending
	pending, err := m.api.MpoolPending(ctx, types.EmptyTSK)
	if err != nil {
		log.Printf("[ERROR] Failed to get mempool pending: %v", err)
		return 0, nil
	}

	return int64(len(pending)), nil
}

// GetStats returns current mempool statistics
func (m *MempoolTracker) GetStats() map[string]interface{} {
	return m.stats.GetStats()
}

// GetAverageSize returns the current average mempool size
func (m *MempoolTracker) GetAverageSize() float64 {
	return m.stats.GetAverageSize()
}

// GetDataPoints returns all recorded data points
func (m *MempoolTracker) GetDataPoints() ([]int64, []time.Time) {
	return m.stats.GetDataPoints()
}

// TrackMempoolOverSimulation tracks mempool size for the entire simulation duration
func TrackMempoolOverSimulation(ctx context.Context, api api.FullNode, duration time.Duration) (*MempoolStats, error) {
	log.Printf("[INFO] Starting mempool tracking for %v", duration)

	// Create tracker with 5-second intervals
	tracker := NewMempoolTracker(api, 5*time.Second)
	tracker.Start()

	// Wait for the specified duration
	select {
	case <-ctx.Done():
		tracker.Stop()
		return tracker.stats, ctx.Err()
	case <-time.After(duration):
		tracker.Stop()
	}

	stats := tracker.GetStats()
	log.Printf("[INFO] Mempool tracking completed:")
	log.Printf("[INFO]   Total measurements: %v", stats["count"])
	log.Printf("[INFO]   Average size: %.2f", stats["average_size"])
	log.Printf("[INFO]   Min size: %v", stats["min_size"])
	log.Printf("[INFO]   Max size: %v", stats["max_size"])

	return tracker.stats, nil
}
