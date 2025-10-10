package resources

import (
	"runtime"
	"time"

	"github.com/antithesishq/antithesis-sdk-go/assert"
)

// EnhanceAssertDetails adds additional context to assertion details
func EnhanceAssertDetails(details map[string]interface{}, nodeName string) map[string]interface{} {
	if details == nil {
		details = make(map[string]interface{})
	}

	// Add node information
	details["node"] = nodeName
	details["node_name"] = nodeName
	details["node_type"] = getNodeType(nodeName)

	// Add caller information
	if _, file, line, ok := runtime.Caller(1); ok {
		details["caller_file"] = file
		details["caller_line"] = line
	}

	// Add timestamp
	details["timestamp"] = time.Now().Format(time.RFC3339)

	return details
}

// AssertAlways is a helper that adds node name to assert.Always
func AssertAlways(nodeName string, condition bool, message string, details map[string]interface{}) {
	details = EnhanceAssertDetails(details, nodeName)
	assert.Always(condition, nodeName+": "+message, details)
}

// AssertSometimes is a helper that adds node name to assert.Sometimes
func AssertSometimes(nodeName string, condition bool, message string, details map[string]interface{}) {
	details = EnhanceAssertDetails(details, nodeName)
	assert.Sometimes(condition, nodeName+": "+message, details)
}

// AssertReachable is a helper that adds node name to assert.Reachable
func AssertReachable(nodeName string, message string, details map[string]interface{}) {
	details = EnhanceAssertDetails(details, nodeName)
	assert.Reachable(nodeName+": "+message, details)
}

// AssertUnreachable is a helper that adds node name to assert.Unreachable
func AssertUnreachable(nodeName string, message string, details map[string]interface{}) {
	details = EnhanceAssertDetails(details, nodeName)
	assert.Unreachable(nodeName+": "+message, details)
}

// getNodeType returns the type of node (Lotus or Forest)
func getNodeType(nodeName string) string {
	switch {
	case nodeName == "Forest":
		return "Forest"
	case nodeName == "Lotus1" || nodeName == "Lotus2":
		return "Lotus"
	default:
		return "Unknown"
	}
}
