package resources

import (
	"runtime"
	"time"
)

// EnhanceAssertDetails adds additional context to assertion details
func EnhanceAssertDetails(details map[string]interface{}, nodeName string) map[string]interface{} {
	// Add node information
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
