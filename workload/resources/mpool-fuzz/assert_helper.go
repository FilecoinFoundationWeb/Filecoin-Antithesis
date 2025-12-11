package mpoolfuzz

import (
	"github.com/antithesishq/antithesis-sdk-go/assert"
)

// AssertSometimes is a helper that adds node name to assert.Sometimes
func AssertSometimes(nodeName string, condition bool, message string, details map[string]interface{}) {
	details["node"] = nodeName
	assert.Sometimes(condition, nodeName+": "+message, details)
}
