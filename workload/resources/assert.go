package resources

import (
	"github.com/antithesishq/antithesis-sdk-go/assert"
)

// Always asserts a property that must hold every time it is checked.
// Use for safety invariants that should never be violated.
func Always(condition bool, name string, details map[string]any) {
	assert.Always(condition, name, details)
}

// Sometimes asserts a property that must hold at least once during the test.
// Use for liveness properties (e.g., "chain eventually advances").
func Sometimes(condition bool, name string, details map[string]any) {
	assert.Sometimes(condition, name, details)
}

// Reachable marks a code path that should be reached during testing.
// Use to verify that workloads exercise important code paths.
func Reachable(name string, details map[string]any) {
	assert.Reachable(name, details)
}

// Unreachable marks a code path that should never be reached.
// Use for error conditions that indicate bugs.
func Unreachable(name string, details map[string]any) {
	assert.Unreachable(name, details)
}

// AlwaysOrUnreachable asserts that if the code path is reached,
// the condition must hold. Use when a check may not always run.
func AlwaysOrUnreachable(condition bool, name string, details map[string]any) {
	assert.AlwaysOrUnreachable(condition, name, details)
}
