package resources

import (
	"context"
	"log"
	"time"

	"github.com/antithesishq/antithesis-sdk-go/assert"
	"github.com/filecoin-project/go-state-types/abi"
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
		MonitorDuration:         30 * time.Second,
		HeightCheckInterval:     7 * time.Second,
		MaxConsecutiveStalls:    3,
	}
}

// NodeHealthMonitor holds the state for monitoring node health
type NodeHealthMonitor struct {
	config            *Config
	monitorConfig     *HealthMonitorConfig
	heightHistory     map[string][]abi.ChainEpoch
	lastTipsetChange  map[string]time.Time
	consecutiveStalls map[string]int
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
		lastTipsetChange:  make(map[string]time.Time),
		consecutiveStalls: make(map[string]int),
	}
}

func CheckNodeSyncStatus(ctx context.Context, config *Config) error {
	nodes := FilterLotusNodes(config.Nodes)
	for _, node := range nodes {
		api, closer, err := ConnectToNode(ctx, node)
		if err != nil {
			log.Printf("failed to connect to node %s: %v", node.Name, err)
			continue
		}
		defer closer()
		syncStatus, err := api.SyncState(ctx)
		if err != nil {
			log.Printf("failed to get sync status: %v", err)
			continue
		}
		log.Printf("Node %s sync status: %+v", node.Name, syncStatus)
		log.Printf("Node %s current height: %d", node.Name, syncStatus.ActiveSyncs[0].Height)
	}
	return nil
}

// ComprehensiveHealthCheck performs all health checks with backoff logic (uses default config)
func ComprehensiveHealthCheck(ctx context.Context, config *Config) error {
	return ComprehensiveHealthCheckWithConfig(ctx, config, DefaultHealthMonitorConfig())
}

// ComprehensiveHealthCheckWithConfig performs health checks based on configuration
func ComprehensiveHealthCheckWithConfig(ctx context.Context, config *Config, monitorConfig *HealthMonitorConfig) error {
	nodes := FilterLotusNodes(config.Nodes)
	if len(nodes) > 0 {
		api, closer, err := ConnectToNode(ctx, nodes[0])
		if err != nil {
			log.Printf("[ERROR] Failed to connect to node for epoch check: %v", err)
			return nil
		}
		defer closer()

		head, err := api.ChainHead(ctx)
		if err != nil {
			log.Printf("[ERROR] Failed to get chain head for epoch check: %v", err)
			return nil
		}

		if head.Height() < 20 {
			log.Printf("[INFO] Current epoch %d is less than required minimum (20). Skipping health checks to avoid false positives.", head.Height())
			return nil
		}
	}

	monitor := NewNodeHealthMonitor(config, monitorConfig)

	log.Printf("[INFO] Starting comprehensive health check with config: %+v", monitorConfig)

	// Check 1: Chain notify monitoring (if enabled)
	if monitorConfig.EnableChainNotify {
		log.Printf("[INFO] Running chain notify check...")
		if err := monitor.CheckChainNotify(ctx); err != nil {
			log.Printf("[ERROR] Chain notify check failed: %v", err)
		}
	} else {
		log.Printf("[INFO] Chain notify check disabled")
	}

	// Check 2: Height monitoring (if enabled)
	if monitorConfig.EnableHeightProgression {
		log.Printf("[INFO] Running height progression check...")
		if err := monitor.CheckHeightProgression(ctx); err != nil {
			log.Printf("[ERROR] Height progression check failed: %v", err)
		}
	} else {
		log.Printf("[INFO] Height progression check disabled")
	}

	// Check 3: Peer count (if enabled)
	if monitorConfig.EnablePeerCount {
		log.Printf("[INFO] Running peer count check...")
		if err := monitor.CheckPeerCount(ctx); err != nil {
			log.Printf("[ERROR] Peer count check failed: %v", err)
		}
	} else {
		log.Printf("[INFO] Peer count check disabled")
	}

	// Check 4: F3 running status (if enabled)
	if monitorConfig.EnableF3Status {
		log.Printf("[INFO] Running F3 status check...")
		if err := monitor.CheckF3Status(ctx); err != nil {
			log.Printf("[ERROR] F3 status check failed: %v", err)
		}
	} else {
		log.Printf("[INFO] F3 status check disabled")
	}

	log.Printf("[INFO] Comprehensive health check completed")
	return nil
}

// CheckChainNotify monitors chain notifications using polling-based approach
func (m *NodeHealthMonitor) CheckChainNotify(ctx context.Context) error {
	log.Printf("[INFO] Starting chain notify monitoring...")
	nodes := FilterV1Nodes(m.config.Nodes)

	// Create a context with cancellation for all goroutines
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Channel to collect errors from goroutines
	errorChan := make(chan error, len(nodes))

	// Launch a goroutine for each node
	for _, node := range nodes {
		node := node // Capture loop variable for goroutine
		go func() {
			if err := m.streamNodeUpdates(ctx, node); err != nil {
				log.Printf("[ERROR] node %s error: %v", node.Name, err)
				errorChan <- nil
			} else {
				errorChan <- nil
			}
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < len(nodes); i++ {
		if err := <-errorChan; err != nil {
			log.Printf("[ERROR] %v", err)
			// Don't return immediately, wait for all nodes
		}
	}

	log.Printf("[INFO] All nodes completed streaming")
	return nil
}

// streamNodeUpdates handles streaming for a single node using polling
func (m *NodeHealthMonitor) streamNodeUpdates(ctx context.Context, node NodeConfig) error {
	api, closer, err := ConnectToNode(ctx, node)
	if err != nil {
		log.Printf("[ERROR] Failed to connect to node '%s': %v", node.Name, err)
		return err
	}
	defer closer()

	// Get initial chain head
	initialHead, err := api.ChainHead(ctx)
	if err != nil {
		log.Printf("[ERROR] Failed to get initial chain head for node '%s': %v", node.Name, err)
		return err
	}

	initialHeight := initialHead.Height() - 5
	log.Printf("[INFO] Node '%s' starting at height: %d, TipSet: %s",
		node.Name, initialHeight, initialHead.Cids())

	// Stream updates for a configurable duration based on monitor config
	monitorDuration := m.monitorConfig.MonitorDuration
	if monitorDuration == 0 {
		monitorDuration = 1 * time.Minute // Default fallback
	}

	log.Printf("[INFO] Node '%s' streaming updates for %v...", node.Name, monitorDuration)

	// Poll every 7 seconds for new blocks
	ticker := time.NewTicker(7 * time.Second)
	defer ticker.Stop()

	timeout := time.After(monitorDuration)
	lastReportedHeight := initialHeight

	for {
		select {
		case <-ctx.Done():
			log.Printf("[INFO] Context cancelled for node '%s'", node.Name)
			return nil
		case <-timeout:
			log.Printf("[INFO] Exiting stream updates for node '%s'", node.Name)
			return nil
		case <-ticker.C:
			currentHead, err := api.ChainHead(ctx)
			if err != nil {
				log.Printf("[WARN] Failed to get current chain head for node '%s': %v", node.Name, err)
				continue
			}

			currentHeight := currentHead.Height()

			// Check if there are new blocks since last check
			if currentHeight > lastReportedHeight {
				heightDiff := currentHeight - lastReportedHeight
				log.Printf("[INFO] Node '%s' advanced %d epochs: %d → %d (TipSet: %s)",
					node.Name, heightDiff, lastReportedHeight, currentHeight, currentHead.Cids())

				lastReportedHeight = currentHeight

				// Update last tipset change time
				m.lastTipsetChange[node.Name] = time.Now()

				// Reset stall counter on activity
				m.consecutiveStalls[node.Name] = 0
			} else {
				// Check if we've been stalled too long
				if lastChange, exists := m.lastTipsetChange[node.Name]; exists {
					if time.Since(lastChange) > monitorDuration {
						assert.Always(false, "Node should be advancing", map[string]interface{}{
							"current_height":       currentHeight,
							"last_reported_height": lastReportedHeight,
							"node":                 node.Name,
							"property":             "Chain advancement",
							"impact":               "Critical - indicates node stalls",
							"stall_duration":       time.Since(lastChange),
						})
					}
				}
			}
		}
	}
}

// CheckHeightProgression monitors height progression for all nodes
func (m *NodeHealthMonitor) CheckHeightProgression(ctx context.Context) error {
	log.Printf("[INFO] Starting height progression monitoring...")

	nodes := FilterV1Nodes(m.config.Nodes)

	for _, node := range nodes {
		go func(node NodeConfig) {
			if err := m.monitorHeightForNode(ctx, node); err != nil {
				log.Printf("[ERROR] Height monitoring failed for node %s: %v", node.Name, err)
			}
		}(node)
	}

	// Monitor for configured duration
	time.Sleep(m.monitorConfig.MonitorDuration)

	return nil
}

// monitorHeightForNode monitors height progression for a specific node
func (m *NodeHealthMonitor) monitorHeightForNode(ctx context.Context, node NodeConfig) error {
	api, closer, err := ConnectToNode(ctx, node)
	if err != nil {
		log.Printf("[ERROR] failed to connect to node %s: %v", node.Name, err)
		return nil
	}
	defer closer()

	// Poll height at configured interval
	ticker := time.NewTicker(m.monitorConfig.HeightCheckInterval)
	defer ticker.Stop()

	timeout := time.After(m.monitorConfig.MonitorDuration)
	checkCount := 0

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timeout:
			return nil
		case <-ticker.C:
			currentHead, err := api.ChainHead(ctx)
			if err != nil {
				log.Printf("[WARN] Failed to get chain head for node %s: %v", node.Name, err)
				continue
			}

			currentHeight := currentHead.Height()
			m.heightHistory[node.Name] = append(m.heightHistory[node.Name], currentHeight)

			// Keep only last 4 heights
			if len(m.heightHistory[node.Name]) > 4 {
				m.heightHistory[node.Name] = m.heightHistory[node.Name][len(m.heightHistory[node.Name])-4:]
			}

			log.Printf("[INFO] Node %s height check %d: %d", node.Name, checkCount+1, currentHeight)

			// Check if height has been the same for 4 consecutive polls
			if len(m.heightHistory[node.Name]) == 4 {
				heights := m.heightHistory[node.Name]
				if heights[0] == heights[1] && heights[1] == heights[2] && heights[2] == heights[3] {
					m.consecutiveStalls[node.Name]++

					if m.consecutiveStalls[node.Name] >= m.monitorConfig.MaxConsecutiveStalls {
						heightIncreasing := false
						assert.Always(heightIncreasing, "Node height should be increasing", map[string]interface{}{
							"node":               node.Name,
							"height":             heights[0],
							"consecutive_stalls": m.consecutiveStalls[node.Name],
							"property":           "Height progression",
							"impact":             "Critical - indicates node is stalled",
						})
					}
				} else {
					// Reset stall counter on height change
					m.consecutiveStalls[node.Name] = 0
				}
			}

			checkCount++
		}
	}
}

// CheckPeerCount checks peer count for all nodes
func (m *NodeHealthMonitor) CheckPeerCount(ctx context.Context) error {
	log.Printf("[INFO] Starting peer count check...")

	nodes := FilterV1Nodes(m.config.Nodes)

	for _, node := range nodes {
		if err := m.checkNodePeerCount(ctx, node); err != nil {
			log.Printf("[ERROR] Peer count check failed for node %s: %v", node.Name, err)
		}
	}

	return nil
}

// checkNodePeerCount checks peer count for a specific node
func (m *NodeHealthMonitor) checkNodePeerCount(ctx context.Context, node NodeConfig) error {
	api, closer, err := ConnectToNode(ctx, node)
	if err != nil {
		log.Printf("[ERROR] failed to connect to node %s: %v", node.Name, err)
		return nil
	}
	defer closer()

	peers, err := api.NetPeers(ctx)
	if err != nil {
		log.Printf("[ERROR] failed to get peers for node %s: %v", node.Name, err)
		return nil
	}

	peerCount := len(peers)
	log.Printf("[INFO] Node %s has %d peers", node.Name, peerCount)

	// Assert that peer count is not 0 or less than 1
	assert.Always(peerCount > 0, "Node should have peers", map[string]interface{}{
		"node":       node.Name,
		"peer_count": peerCount,
		"property":   "Peer connectivity",
		"impact":     "Critical - indicates network isolation",
	})

	return nil
}

// CheckF3Status checks if F3 is running on all nodes
func (m *NodeHealthMonitor) CheckF3Status(ctx context.Context) error {
	log.Printf("[INFO] Starting F3 status check...")

	nodes := FilterV1Nodes(m.config.Nodes)

	for _, node := range nodes {
		if err := m.checkNodeF3Status(ctx, node); err != nil {
			log.Printf("[ERROR] F3 status check failed for node %s: %v", node.Name, err)
		}
	}

	return nil
}

// checkNodeF3Status checks F3 status for a specific node
func (m *NodeHealthMonitor) checkNodeF3Status(ctx context.Context, node NodeConfig) error {
	api, closer, err := ConnectToNode(ctx, node)
	if err != nil {
		log.Printf("[ERROR] failed to connect to node %s: %v", node.Name, err)
		return nil
	}
	defer closer()

	// Try to call F3IsRunning method
	// Note: This method might not exist on all API versions, so we'll handle the error gracefully
	f3Running, err := api.F3IsRunning(ctx)
	if err != nil {
		log.Printf("[ERROR] failed to get F3 status for node %s: %v", node.Name, err)
		return nil
	}

	log.Printf("[INFO] Node %s F3 status: %v", node.Name, f3Running)

	// Assert that F3 is running
	assert.Always(f3Running, "F3 should be running", map[string]interface{}{
		"node":       node.Name,
		"f3_running": f3Running,
		"property":   "F3 service status",
		"impact":     "Critical - F3 is required for consensus",
	})

	return nil
}
