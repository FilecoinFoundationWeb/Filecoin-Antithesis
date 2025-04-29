package p2pfuzz

// PingAttackType defines the different malicious ping strategies.
type PingAttackType int

const (
	RandomPayload PingAttackType = iota
	OversizedPayload
	EmptyPayload
	MultipleStreams
	IncompleteWrite
	PingBarrage       // Sends many pings in rapid succession
	MalformedPayload  // Sends structurally invalid data
	ConnectDisconnect // Rapidly connects and disconnects
	VariablePayload   // Sends payloads of varying sizes
	SlowWrite         // Writes data very slowly
	// PubSub Attacks
	PubSubIHaveSpam
	PubSubGraftPruneSpam
	PubSubMalformedMsg
	PubSubTopicFlood
)

// AttackTypeFromString converts a string identifier to a PingAttackType.
func AttackTypeFromString(attackType string) PingAttackType {
	switch attackType {
	case "random":
		return RandomPayload
	case "oversized":
		return OversizedPayload
	case "empty":
		return EmptyPayload
	case "multiple":
		return MultipleStreams
	case "incomplete":
		return IncompleteWrite
	case "barrage":
		return PingBarrage
	case "malformed":
		return MalformedPayload
	case "connectdisconnect":
		return ConnectDisconnect
	case "variable":
		return VariablePayload
	case "slow":
		return SlowWrite
	default:
		return RandomPayload // Default to random if unknown
	}
}

// GetAttackTypes returns a list of valid string identifiers for attack types.
func GetAttackTypes() []string {
	return []string{
		"random",
		"oversized",
		"empty",
		"multiple",
		"incomplete",
		"barrage",
		"malformed",
		"connectdisconnect",
		"variable",
		"slow",
	}
}
