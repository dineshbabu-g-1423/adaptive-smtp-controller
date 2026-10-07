// Package config loads YAML configuration for limits, IP pools and warmup.
package config

import (
	"os"

	"github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/routing"
	"github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/throttle"
	"github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/warmup"
	"gopkg.in/yaml.v3"
)

// Limits mirrors configs/limits.yaml.
type Limits struct {
	Throttle throttle.Config `yaml:"throttle"`
}

// Pools mirrors configs/providers.yaml (IP pools + seeded reputation).
type Pools struct {
	IPs        []routing.IP `yaml:"ips"`
	Reputation []RepSeed    `yaml:"reputation"`
}

// RepSeed seeds an (IP, provider) reputation score for demos.
type RepSeed struct {
	IP       string  `yaml:"ip"`
	Provider string  `yaml:"provider"`
	Score    float64 `yaml:"score"`
}

// Warmup mirrors configs/warmup.yaml.
type Warmup struct {
	Thresholds warmup.Thresholds `yaml:"thresholds"`
	Schedules  []warmup.Schedule `yaml:"schedules"`
}

// Load reads and unmarshals a YAML file into dst.
func Load(path string, dst any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(b, dst)
}
