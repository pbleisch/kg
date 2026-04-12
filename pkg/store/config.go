package store

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config holds settings from .kg/config.yaml.
type Config struct {
	Backend   string           `yaml:"backend"`             // "ladybug" (only supported backend)
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
			return &Config{Backend: "ladybug"}, nil
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
// Returns "ladybug" always (only supported backend).
func readBackendConfig(kgDir string) string {
	data, err := os.ReadFile(filepath.Join(kgDir, "config.yaml"))
	if err != nil {
		return "ladybug"
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return "ladybug"
	}
	b := strings.TrimSpace(strings.ToLower(cfg.Backend))
	if b == "" || b == "auto" || b == "bolt" {
		return "ladybug"
	}
	return b
}

// ActiveBackend returns "ladybug" (the only supported backend).
func ActiveBackend(_ string) string {
	return "ladybug"
}

// openIndexDirect bypasses daemon detection in OpenIndex. Set to true by
// the daemon process before opening its own index.
var openIndexDirect bool

// SetOpenIndexDirect enables or disables direct index opening, bypassing
// daemon detection. The daemon process calls this before opening its own index.
func SetOpenIndexDirect(v bool) { openIndexDirect = v }

// OpenIndex opens or creates an index at <kgDir>/.cache/. Returns an Indexer
// backed by LadybugDB.
func OpenIndex(kgDir string) (Indexer, error) {
	// If a daemon is serving this kgDir, connect via RPC instead of
	// opening the index file directly. This eliminates lock contention.
	if !openIndexDirect {
		absDir, _ := filepath.Abs(kgDir)
		sockPath := rpcSocketPath(absDir)
		if DaemonAvailable(sockPath) {
			remote, err := DialRemote(sockPath)
			if err == nil {
				return remote, nil
			}
			// Daemon appeared available but dial failed — fall back to direct.
			log.Printf("WARNING: daemon available but dial failed: %v; falling back to direct index", err)
		}
		// No daemon running — try to start one in the background.
		// This command uses direct open; the NEXT command will use RPC.
		autoStartDaemon(absDir)
	}

	return openLadybug(kgDir)
}
