package resources

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/filecoin-project/go-state-types/abi"
)

const (
	// MinHeightForHealthCheck is the minimum chain height required before running health checks
	MinHeightForHealthCheck = 20
	// MinHeightForF3Check is the minimum chain height required for F3 status check
	MinHeightForF3Check = 10
	// DefaultMonitorDuration is the default duration for monitoring operations
	DefaultMonitorDuration = 180 * time.Second
	// DefaultHeightCheckInterval is the default interval between height checks
	DefaultHeightCheckInterval = 7 * time.Second
	// DefaultMaxConsecutiveStalls is the default max stalls before alerting
	DefaultMaxConsecutiveStalls = 3
	// HeightHistorySize is the number of heights to keep in history
	HeightHistorySize = 4
)

// HealthMonitorConfig holds configuration for individual health checks
type HealthMonitorConfig struct {
	EnableChainNotify       bool
	EnableHeightProgression bool
	EnablePeerCount         bool
	EnableF3Status          bool
	MonitorDuration         time.Duration
	HeightCheckInterval     time.Duration
	MaxConsecutiveStalls    int
}

// DefaultHealthMonitorConfig returns a default configuration
func DefaultHealthMonitorConfig() *HealthMonitorConfig {
	return &HealthMonitorConfig{
		EnableChainNotify:       true,
		EnableHeightProgression: true,
		EnablePeerCount:         true,
		EnableF3Status:          true,
		MonitorDuration:         DefaultMonitorDuration,
		HeightCheckInterval:     DefaultHeightCheckInterval,
		MaxConsecutiveStalls:    DefaultMaxConsecutiveStalls,
	}
}

// NodeHealthMonitor holds the state for monitoring node health
type NodeHealthMonitor struct {
	config            *Config
	monitorConfig     *HealthMonitorConfig
	heightHistory     map[string][]abi.ChainEpoch
	consecutiveStalls map[string]int
	mu                sync.RWMutex
}

// NewNodeHealthMonitor creates a new health monitor instance
func NewNodeHealthMonitor(config *Config, monitorConfig *HealthMonitorConfig) *NodeHealthMonitor {
	if monitorConfig == nil {
		monitorConfig = DefaultHealthMonitorConfig()
	}

	return &NodeHealthMonitor{
		config:            config,
		monitorConfig:     monitorConfig,
		heightHistory:     make(map[string][]abi.ChainEpoch),
		consecutiveStalls: make(map[string]int),
	}
}

// ComprehensiveHealthCheck performs all health checks (uses default config)
func ComprehensiveHealthCheck(ctx context.Context, config *Config) error {
	return ComprehensiveHealthCheckWithConfig(ctx, config, DefaultHealthMonitorConfig())
}

// ComprehensiveHealthCheckWithConfig performs health checks based on configuration
func ComprehensiveHealthCheckWithConfig(ctx context.Context, config *Config, monitorConfig *HealthMonitorConfig) error {
	nodes := FilterV1Nodes(config.Nodes)
	if len(nodes) == 0 {
		return fmt.Errorf("no nodes available for health check")
	}

	// Check if chain height is sufficient
	api, closer, err := ConnectToNode(ctx, nodes[0])
	if err != nil {
		return fmt.Errorf("failed to connect for epoch check: %w", err)
	}

	head, err := api.ChainHead(ctx)
	closer()
	if err != nil {
		return fmt.Errorf("failed to get chain head: %w", err)
	}

	if head.Height() < MinHeightForHealthCheck {
		log.Printf("[INFO] Current epoch %d < %d, skipping health checks", head.Height(), MinHeightForHealthCheck)
		return nil
	}

	monitor := NewNodeHealthMonitor(config, monitorConfig)

	log.Printf("[INFO] Starting comprehensive health check...")

	var wg sync.WaitGroup
	errChan := make(chan error, 4)

	// Run enabled checks concurrently
	if monitorConfig.EnablePeerCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := monitor.CheckPeerCount(ctx); err != nil {
				errChan <- fmt.Errorf("peer count: %w", err)
			}
		}()
	}

	if monitorConfig.EnableF3Status {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := monitor.CheckF3Status(ctx); err != nil {
				errChan <- fmt.Errorf("F3 status: %w", err)
			}
		}()
	}

	if monitorConfig.EnableHeightProgression {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := monitor.CheckHeightProgression(ctx); err != nil {
				errChan <- fmt.Errorf("height progression: %w", err)
			}
		}()
	}

	if monitorConfig.EnableChainNotify {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := monitor.CheckChainNotify(ctx); err != nil {
				errChan <- fmt.Errorf("chain notify: %w", err)
			}
		}()
	}

	wg.Wait()
	close(errChan)

	// Collect errors
	var errors []error
	for err := range errChan {
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		log.Printf("[WARN] Health check completed with %d errors: %v", len(errors), errors)
	} else {
		log.Printf("[INFO] Comprehensive health check completed successfully")
	}

	return nil
}

// CheckChainNotify monitors chain notifications using polling
func (m *NodeHealthMonitor) CheckChainNotify(ctx context.Context) error {
	log.Printf("[INFO] Starting chain notify monitoring for %v...", m.monitorConfig.MonitorDuration)
	nodes := FilterV1Nodes(m.config.Nodes)

	var wg sync.WaitGroup
	ctx, cancel := context.WithTimeout(ctx, m.monitorConfig.MonitorDuration)
	defer cancel()

	for _, node := range nodes {
		wg.Add(1)
		go func(n NodeConfig) {
			defer wg.Done()
			m.monitorNodeChain(ctx, n)
		}(node)
	}

	wg.Wait()
	log.Printf("[INFO] Chain notify monitoring completed")
	return nil
}

// monitorNodeChain monitors a single node's chain progression
func (m *NodeHealthMonitor) monitorNodeChain(ctx context.Context, node NodeConfig) {
	api, closer, err := ConnectToNode(ctx, node)
	if err != nil {
		log.Printf("[ERROR] Failed to connect to %s: %v", node.Name, err)
		return
	}
	defer closer()

	initialHead, err := api.ChainHead(ctx)
	if err != nil {
		log.Printf("[ERROR] Failed to get initial head for %s: %v", node.Name, err)
		return
	}

	lastHeight := initialHead.Height()
	lastAdvance := time.Now()
	ticker := time.NewTicker(DefaultHeightCheckInterval)
	defer ticker.Stop()

	log.Printf("[INFO] Node %s starting at height %d", node.Name, lastHeight)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			currentHead, err := api.ChainHead(ctx)
			if err != nil {
				log.Printf("[WARN] Failed to get head for %s: %v", node.Name, err)
				continue
			}

			currentHeight := currentHead.Height()
			if currentHeight > lastHeight {
				log.Printf("[INFO] Node %s: %d → %d (+%d)",
					node.Name, lastHeight, currentHeight, currentHeight-lastHeight)
				lastHeight = currentHeight
				lastAdvance = time.Now()
			} else if time.Since(lastAdvance) > m.monitorConfig.MonitorDuration/2 {
				AssertAlways(node.Name, false, "Chain should be advancing",
					map[string]interface{}{
						"height":         currentHeight,
						"stall_duration": time.Since(lastAdvance).String(),
					})
			}
		}
	}
}

// CheckHeightProgression monitors height progression for all nodes
func (m *NodeHealthMonitor) CheckHeightProgression(ctx context.Context) error {
	log.Printf("[INFO] Starting height progression monitoring...")
	nodes := FilterV1Nodes(m.config.Nodes)

	var wg sync.WaitGroup
	ctx, cancel := context.WithTimeout(ctx, m.monitorConfig.MonitorDuration)
	defer cancel()

	for _, node := range nodes {
		wg.Add(1)
		go func(n NodeConfig) {
			defer wg.Done()
			m.monitorHeightForNode(ctx, n)
		}(node)
	}

	wg.Wait()
	log.Printf("[INFO] Height progression monitoring completed")
	return nil
}

// monitorHeightForNode monitors height progression for a specific node
func (m *NodeHealthMonitor) monitorHeightForNode(ctx context.Context, node NodeConfig) {
	api, closer, err := ConnectToNode(ctx, node)
	if err != nil {
		log.Printf("[ERROR] Failed to connect to %s: %v", node.Name, err)
		return
	}
	defer closer()

	ticker := time.NewTicker(m.monitorConfig.HeightCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			currentHead, err := api.ChainHead(ctx)
			if err != nil {
				log.Printf("[WARN] Failed to get head for %s: %v", node.Name, err)
				continue
			}

			currentHeight := currentHead.Height()
			m.recordHeight(node.Name, currentHeight)

			if m.isStalled(node.Name) {
				stalls := m.getStallCount(node.Name)
				if stalls >= m.monitorConfig.MaxConsecutiveStalls {
					AssertAlways(node.Name, false, "Height should be increasing",
						map[string]interface{}{
							"height":             currentHeight,
							"consecutive_stalls": stalls,
						})
				}
			}
		}
	}
}

// recordHeight records a height measurement and checks for stalls
func (m *NodeHealthMonitor) recordHeight(nodeName string, height abi.ChainEpoch) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.heightHistory[nodeName] = append(m.heightHistory[nodeName], height)

	// Keep only last N heights
	if len(m.heightHistory[nodeName]) > HeightHistorySize {
		m.heightHistory[nodeName] = m.heightHistory[nodeName][1:]
	}
}

// isStalled checks if a node's height has been static
func (m *NodeHealthMonitor) isStalled(nodeName string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	heights := m.heightHistory[nodeName]
	if len(heights) < HeightHistorySize {
		return false
	}

	// Check if all heights are the same
	for i := 1; i < len(heights); i++ {
		if heights[i] != heights[0] {
			m.consecutiveStalls[nodeName] = 0
			return false
		}
	}

	m.consecutiveStalls[nodeName]++
	return true
}

// getStallCount returns the consecutive stall count for a node
func (m *NodeHealthMonitor) getStallCount(nodeName string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.consecutiveStalls[nodeName]
}

// CheckPeerCount checks peer count for all nodes
func (m *NodeHealthMonitor) CheckPeerCount(ctx context.Context) error {
	log.Printf("[INFO] Checking peer counts...")
	nodes := FilterV1Nodes(m.config.Nodes)

	for _, node := range nodes {
		if err := m.checkNodePeerCount(ctx, node); err != nil {
			log.Printf("[ERROR] Peer count check failed for %s: %v", node.Name, err)
		}
	}

	return nil
}

// checkNodePeerCount checks peer count for a specific node
func (m *NodeHealthMonitor) checkNodePeerCount(ctx context.Context, node NodeConfig) error {
	api, closer, err := ConnectToNode(ctx, node)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer closer()

	peers, err := api.NetPeers(ctx)
	if err != nil {
		return fmt.Errorf("failed to get peers: %w", err)
	}

	peerCount := len(peers)
	log.Printf("[INFO] Node %s has %d peers", node.Name, peerCount)

	AssertAlways(node.Name, peerCount > 0, "Node should have peers",
		map[string]interface{}{
			"peer_count": peerCount,
		})

	return nil
}

// CheckF3Status checks if F3 is running on all nodes
func (m *NodeHealthMonitor) CheckF3Status(ctx context.Context) error {
	log.Printf("[INFO] Checking F3 status...")
	nodes := FilterV1Nodes(m.config.Nodes)

	for _, node := range nodes {
		if err := m.checkNodeF3Status(ctx, node); err != nil {
			log.Printf("[ERROR] F3 status check failed for %s: %v", node.Name, err)
		}
	}

	return nil
}

// checkNodeF3Status checks F3 status for a specific node
func (m *NodeHealthMonitor) checkNodeF3Status(ctx context.Context, node NodeConfig) error {
	api, closer, err := ConnectToNode(ctx, node)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer closer()

	head, err := api.ChainHead(ctx)
	if err != nil {
		return fmt.Errorf("failed to get chain head: %w", err)
	}

	if head.Height() < MinHeightForF3Check {
		log.Printf("[INFO] Node %s height %d < %d, skipping F3 check", node.Name, head.Height(), MinHeightForF3Check)
		return nil
	}

	f3Running, err := api.F3IsRunning(ctx)
	if err != nil {
		return fmt.Errorf("F3IsRunning failed: %w", err)
	}

	log.Printf("[INFO] Node %s F3 running: %v", node.Name, f3Running)

	AssertAlways(node.Name, f3Running, "F3 should be running",
		map[string]interface{}{
			"f3_running": f3Running,
		})

	return nil
}
