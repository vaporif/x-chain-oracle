package engine

import (
	"fmt"

	"github.com/knadh/koanf/parsers/toml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

func LoadRules(path string) (*RulesConfig, error) {
	k := koanf.New(".")
	if err := k.Load(file.Provider(path), toml.Parser()); err != nil {
		return nil, err
	}

	var cfg RulesConfig
	if err := k.Unmarshal("", &cfg); err != nil {
		return nil, err
	}

	for _, corr := range cfg.Correlations {
		if len(corr.Sequence) != 2 {
			return nil, fmt.Errorf("correlation %q: sequence must have exactly 2 elements, got %d",
				corr.Name, len(corr.Sequence))
		}
	}

	return &cfg, nil
}
