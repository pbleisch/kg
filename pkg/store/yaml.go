package store

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ParseEntityID splits an ID like "person:alice" into (typeName, localID).
func ParseEntityID(id string) (typeName, localID string, err error) {
	parts := strings.SplitN(id, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid entity ID %q: expected type:local-id", id)
	}
	return parts[0], parts[1], nil
}

// encodeLocalID percent-encodes a local-id for use in filenames.
func encodeLocalID(localID string) string {
	// Encode characters that are not filesystem-safe.
	return url.PathEscape(localID)
}

// EntityPath returns the filesystem path for an entity's YAML file.
func EntityPath(kgDir, id string) string {
	typeName, localID, err := ParseEntityID(id)
	if err != nil {
		return ""
	}
	return filepath.Join(kgDir, "entities", typeName, encodeLocalID(localID)+".yaml")
}

// ReadEntity reads and parses an entity YAML file.
func ReadEntity(kgDir, id string) (*Entity, error) {
	path := EntityPath(kgDir, id)
	if path == "" {
		return nil, fmt.Errorf("invalid entity ID %q", id)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading entity %q: %w", id, err)
	}

	var e Entity
	if err := yaml.Unmarshal(data, &e); err != nil {
		return nil, fmt.Errorf("parsing entity %q: %w", id, err)
	}
	return &e, nil
}

// WriteEntity marshals an entity to YAML and writes it to the correct path.
func WriteEntity(kgDir string, e *Entity) error {
	path := EntityPath(kgDir, e.ID)
	if path == "" {
		return fmt.Errorf("invalid entity ID %q", e.ID)
	}

	journalNote(path)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating directory for entity %q: %w", e.ID, err)
	}

	data, err := yaml.Marshal(e)
	if err != nil {
		return fmt.Errorf("marshaling entity %q: %w", e.ID, err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing entity %q: %w", e.ID, err)
	}
	return nil
}

// DeleteEntity removes an entity's YAML file.
func DeleteEntity(kgDir, id string) error {
	path := EntityPath(kgDir, id)
	if path == "" {
		return fmt.Errorf("invalid entity ID %q", id)
	}

	journalNote(path)

	if err := os.Remove(path); err != nil {
		return fmt.Errorf("deleting entity %q: %w", id, err)
	}
	return nil
}

// ListEntities walks .kg/entities/ and reads all entity files.
func ListEntities(kgDir string) ([]*Entity, error) {
	entitiesDir := filepath.Join(kgDir, "entities")
	return walkEntities(entitiesDir)
}

// ListEntitiesByType lists entities of a single type.
func ListEntitiesByType(kgDir, typeName string) ([]*Entity, error) {
	typeDir := filepath.Join(kgDir, "entities", typeName)
	return walkEntities(typeDir)
}

// walkEntities reads all .yaml files under dir.
func walkEntities(dir string) ([]*Entity, error) {
	var entities []*Entity

	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".yaml") {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}

		var e Entity
		if err := yaml.Unmarshal(data, &e); err != nil {
			return fmt.Errorf("parsing %s: %w", path, err)
		}
		entities = append(entities, &e)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return entities, nil
}
