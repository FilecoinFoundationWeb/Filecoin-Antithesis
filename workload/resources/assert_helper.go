package resources

import (
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

// getNodeType returns the type of node based on prefix matching
func getNodeType(nodeName string) string {
	if len(nodeName) >= 6 && nodeName[:6] == "Forest" {
		return "Forest"
	}
	if len(nodeName) >= 5 && nodeName[:5] == "Lotus" {
		return "Lotus"
	}
	if len(nodeName) >= 5 && nodeName[:5] == "Curio" {
		return "Curio"
	}
	return "Unknown"
}
