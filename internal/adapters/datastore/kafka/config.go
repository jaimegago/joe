package kafka

import (
	"encoding/json"
	"fmt"
)

// Config holds Kafka adapter configuration.
type Config struct {
	Brokers       []string `json:"brokers"`
	TLSEnabled    bool     `json:"tls_enabled"`
	SASLMechanism string   `json:"sasl_mechanism"` // PLAIN, SCRAM-SHA-256, SCRAM-SHA-512
	Username      string   `json:"username"`
	Password      string   `json:"password"`
}

// ParseConfig parses a component config map into a Config.
func ParseConfig(raw map[string]any) (Config, error) {
	var cfg Config

	data, err := json.Marshal(raw)
	if err != nil {
		return cfg, fmt.Errorf("marshal config: %w", err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("unmarshal config: %w", err)
	}

	if len(cfg.Brokers) == 0 {
		return cfg, fmt.Errorf("brokers is required and must be non-empty")
	}

	return cfg, nil
}
