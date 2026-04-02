package engine

import "time"

type Condition struct {
	Field string `koanf:"field" toml:"field"`
	Op    string `koanf:"op" toml:"op"`
	Value string `koanf:"value" toml:"value"`
}

type Rule struct {
	Name           string      `koanf:"name" toml:"name"`
	Trigger        string      `koanf:"trigger" toml:"trigger"`
	Conditions     []Condition `koanf:"conditions" toml:"conditions"`
	Signal         string      `koanf:"signal" toml:"signal"`
	Confidence     float64     `koanf:"confidence" toml:"confidence"`
	MetadataFields []string    `koanf:"metadata_fields" toml:"metadata_fields"`
}

type Correlation struct {
	Name          string   `koanf:"name" toml:"name"`
	Sequence      []string `koanf:"sequence" toml:"sequence"`
	Window        string   `koanf:"window" toml:"window"`
	SameFields    []string `koanf:"same_fields" toml:"same_fields"`
	Signal        string   `koanf:"signal" toml:"signal"`
	Confidence    float64  `koanf:"confidence" toml:"confidence"`
	MinFirstCount int      `koanf:"min_first_count" toml:"min_first_count"`

	windowDuration time.Duration
}

type RulesConfig struct {
	Rules        []Rule        `koanf:"rules" toml:"rules"`
	Correlations []Correlation `koanf:"correlations" toml:"correlations"`
}
