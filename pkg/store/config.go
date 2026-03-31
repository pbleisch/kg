package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config holds settings from .kg/config.yaml.
type Config struct {
	Backend   string           `yaml:"backend"`             // "auto", "bolt", or "ladybug"
	Embedding *EmbeddingConfig `yaml:"embedding,omitempty"` // optional embedding settings
}

// EmbeddingConfig configures automatic embedding generation.
type EmbeddingConfig struct {
	Command string   `yaml:"command"`          // external command to run
	Fields  []string `yaml:"fields"`           // entity attributes to embed
	Batch   bool     `yaml:"batch,omitempty"`  // use batch protocol
}

// LoadConfig reads and returns the full config from .kg/config.yaml.
// Returns a default config if the file doesn't exist.
func LoadConfig(kgDir string) (*Config, error) {
	data, err := os.ReadFile(filepath.Join(kgDir, "config.yaml"))
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{Backend: "auto"}, nil
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	return &cfg, nil
}

// readBackendConfig reads the backend setting from .kg/config.yaml.
// Returns "auto" if the file doesn't exist or the field is empty.
func readBackendConfig(kgDir string) string {
	data, err := os.ReadFile(filepath.Join(kgDir, "config.yaml"))
	if err != nil {
		return "auto"
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return "auto"
	}
	b := strings.TrimSpace(strings.ToLower(cfg.Backend))
	if b == "" {
		return "auto"
	}
	return b
}

// ActiveBackend returns the name of the backend that OpenIndex would select.
func ActiveBackend(kgDir string) string {
	backend := readBackendConfig(kgDir)
	switch backend {
	case "ladybug":
		if ladybugAvailable {
			return "ladybug"
		}
		return "ladybug (unavailable)"
	case "bolt":
		return "bolt"
	default:
		if ladybugAvailable {
			return "ladybug"
		}
		return "bolt"
	}
}
