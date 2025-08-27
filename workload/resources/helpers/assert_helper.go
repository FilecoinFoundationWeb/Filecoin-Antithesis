package helpers

import (
	"runtime"
	"time"

	"github.com/antithesishq/antithesis-sdk-go/assert"
)

type AssertionType string

const (
	AssertAlways    AssertionType = "always"
	AssertSometimes AssertionType = "sometimes"
	AssertNever     AssertionType = "never"
)

// AssertWithContext performs assertions with enhanced context and custom messages
func AssertWithContext(
	assertType AssertionType,
	condition bool,
	message string,
	nodeName string,
	details map[string]interface{},
) {
	if details == nil {
		details = make(map[string]interface{})
	}

	// Add standard metadata
	details["node_name"] = nodeName
	details["timestamp"] = time.Now().Format(time.RFC3339)

	// Add caller information
	if _, file, line, ok := runtime.Caller(1); ok {
		details["caller_file"] = file
		details["caller_line"] = line
	}

	if _, ok := details["property"]; !ok {
		details["property"] = "General assertion"
	}
	if _, ok := details["impact"]; !ok {
		details["impact"] = "Unknown"
	}
	if _, ok := details["details"]; !ok {
		details["details"] = message
	}

	// Perform the appropriate type of assertion
	switch assertType {
	case AssertAlways:
		assert.Always(condition, message, details)
	case AssertSometimes:
		assert.Sometimes(condition, message, details)
	default:
		assert.Sometimes(condition, message, details) // Default to Sometimes
	}
}
