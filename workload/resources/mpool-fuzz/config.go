package mpoolfuzz

// AttackType represents the type of attack to perform
type AttackType string

const (
	// SimpleAttack represents a basic message pool attack
	SimpleAttack AttackType = "simple"
	// ChainedAttack represents a chained message attack
	ChainedAttack AttackType = "chained"
)

type Config struct {
	Count      int
	Seed       int64
	Concurrent int
	AttackType AttackType
}

func DefaultConfig() *Config {
	return &Config{
		Count:      100,
		Seed:       1,
		Concurrent: 5,
		AttackType: SimpleAttack,
	}
}
