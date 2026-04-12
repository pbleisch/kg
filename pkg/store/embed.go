package store

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// EmbedManifest tracks which entity-field pairs have been embedded.
type EmbedManifest struct {
	Version    int                       `json:"version"`
	EmbeddedAt time.Time                 `json:"embedded_at"`
	Entries    map[string]EmbedEntrySet  `json:"entries"` // entity ID → field entries
}

// EmbedEntrySet maps field names to their embed tracking info.
type EmbedEntrySet map[string]EmbedEntry

// EmbedEntry records the content checksum and vector dimensions for one
// entity-field embedding.
type EmbedEntry struct {
	Checksum string `json:"checksum"`
	Dim      int    `json:"dim"`
}

// EmbedInput is the JSON payload sent to the external embedding command.
type EmbedInput struct {
	Text     string `json:"text"`
	Field    string `json:"field"`
	EntityID string `json:"entity_id"`
	Type     string `json:"type"`
}

// EmbedResult collects statistics from an embed operation.
type EmbedResult struct {
	Embedded       int      `json:"embedded"`
	Skipped        int      `json:"skipped"`
	Errors         int      `json:"errors"`
	Warnings       []string `json:"warnings,omitempty"`
	FailedEntities []string `json:"failed_entities,omitempty"`
}

func embedManifestPath(kgDir string) string {
	return filepath.Join(kgDir, ".cache", "embed-manifest.json")
}

// LoadEmbedManifest reads the embed manifest. Returns an empty manifest
// if the file doesn't exist.
func LoadEmbedManifest(kgDir string) (*EmbedManifest, error) {
	data, err := os.ReadFile(embedManifestPath(kgDir))
	if err != nil {
		if os.IsNotExist(err) {
			return &EmbedManifest{
				Version: 1,
				Entries: make(map[string]EmbedEntrySet),
			}, nil
		}
		return nil, fmt.Errorf("reading embed manifest: %w", err)
	}
	var m EmbedManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing embed manifest: %w", err)
	}
	if m.Entries == nil {
		m.Entries = make(map[string]EmbedEntrySet)
	}
	return &m, nil
}

// SaveEmbedManifest writes the embed manifest to disk.
func SaveEmbedManifest(kgDir string, m *EmbedManifest) error {
	p := embedManifestPath(kgDir)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("creating cache dir: %w", err)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling embed manifest: %w", err)
	}
	return os.WriteFile(p, data, 0o644)
}

// TextChecksum returns the SHA-256 hex digest of a string value.
func TextChecksum(text string) string {
	h := sha256.Sum256([]byte(text))
	return fmt.Sprintf("%x", h)
}

// Embed processes entities that need embedding updates. If entityIDs is
// empty, all entities are scanned. Otherwise only the specified entities
// are processed.
func Embed(kgDir string, rebuild bool, dryRun bool, entityIDs ...string) (*EmbedResult, error) {
	cfg, err := LoadConfig(kgDir)
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}
	if cfg.Embedding == nil || cfg.Embedding.Command == "" {
		return nil, fmt.Errorf("no embedding command configured in config.yaml")
	}
	ecfg := cfg.Embedding

	manifest, err := LoadEmbedManifest(kgDir)
	if err != nil {
		return nil, fmt.Errorf("loading embed manifest: %w", err)
	}
	if rebuild {
		manifest.Entries = make(map[string]EmbedEntrySet)
	}

	// Collect entities to process.
	var entities []*Entity
	if len(entityIDs) > 0 {
		for _, id := range entityIDs {
			e, err := ReadEntity(kgDir, id)
			if err != nil {
				continue // entity may have been deleted
			}
			entities = append(entities, e)
		}
	} else {
		entities, err = ListEntities(kgDir)
		if err != nil {
			return nil, fmt.Errorf("listing entities: %w", err)
		}
	}

	result := &EmbedResult{}

	// Collect stale items.
	type staleItem struct {
		entity *Entity
		field  string
		text   string
		cksum  string
	}
	var stale []staleItem

	for _, e := range entities {
		for _, field := range ecfg.Fields {
			val, ok := e.Attributes[field]
			if !ok {
				continue
			}
			text := fmt.Sprintf("%v", val)
			if text == "" {
				continue
			}
			cksum := TextChecksum(text)

			if entries, ok := manifest.Entries[e.ID]; ok {
				if entry, ok := entries[field]; ok && entry.Checksum == cksum {
					result.Skipped++
					continue
				}
			}

			stale = append(stale, staleItem{
				entity: e,
				field:  field,
				text:   text,
				cksum:  cksum,
			})
		}
	}

	if dryRun || len(stale) == 0 {
		result.Embedded = len(stale)
		return result, nil
	}

	// Run embedder.
	if ecfg.Batch && len(stale) > 0 {
		inputs := make([]EmbedInput, len(stale))
		for i, s := range stale {
			inputs[i] = EmbedInput{
				Text:     s.text,
				Field:    s.field,
				EntityID: s.entity.ID,
				Type:     s.entity.Type,
			}
		}
		vectors, err := RunEmbedBatch(ecfg.Command, inputs)
		if err != nil {
			for _, s := range stale {
				result.FailedEntities = append(result.FailedEntities, s.entity.ID)
			}
			return result, fmt.Errorf("batch embed: %w", err)
		}
		if len(vectors) != len(stale) {
			for _, s := range stale {
				result.FailedEntities = append(result.FailedEntities, s.entity.ID)
			}
			return result, fmt.Errorf("batch embed returned %d vectors, expected %d", len(vectors), len(stale))
		}
		for i, s := range stale {
			embField := s.field + "_embedding"
			s.entity.Attributes[embField] = vectors[i]
			if err := WriteEntity(kgDir, s.entity); err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("%s: write: %v", s.entity.ID, err))
				result.Errors++
				result.FailedEntities = append(result.FailedEntities, s.entity.ID)
				continue
			}
			if manifest.Entries[s.entity.ID] == nil {
				manifest.Entries[s.entity.ID] = make(EmbedEntrySet)
			}
			manifest.Entries[s.entity.ID][s.field] = EmbedEntry{
				Checksum: s.cksum,
				Dim:      len(vectors[i]),
			}
			result.Embedded++
		}
	} else {
		for _, s := range stale {
			input := EmbedInput{
				Text:     s.text,
				Field:    s.field,
				EntityID: s.entity.ID,
				Type:     s.entity.Type,
			}
			vec, err := RunEmbedSingle(ecfg.Command, input)
			if err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("%s/%s: %v", s.entity.ID, s.field, err))
				result.Errors++
				result.FailedEntities = append(result.FailedEntities, s.entity.ID)
				continue
			}
			embField := s.field + "_embedding"
			s.entity.Attributes[embField] = vec
			if err := WriteEntity(kgDir, s.entity); err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("%s: write: %v", s.entity.ID, err))
				result.Errors++
				result.FailedEntities = append(result.FailedEntities, s.entity.ID)
				continue
			}
			if manifest.Entries[s.entity.ID] == nil {
				manifest.Entries[s.entity.ID] = make(EmbedEntrySet)
			}
			manifest.Entries[s.entity.ID][s.field] = EmbedEntry{
				Checksum: s.cksum,
				Dim:      len(vec),
			}
			result.Embedded++
		}
	}

	manifest.EmbeddedAt = time.Now()
	if err := SaveEmbedManifest(kgDir, manifest); err != nil {
		return result, fmt.Errorf("saving embed manifest: %w", err)
	}

	return result, nil
}
