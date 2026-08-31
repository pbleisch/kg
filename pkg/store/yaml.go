package store

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
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

// FindByType reads one type's directory and returns the entities matching the
// query options.
//
// It deliberately does not build a Graph: a typed find needs one directory,
// and making it pay for the whole corpus is how a read that should be cheap
// stops being cheap.
func FindByType(kgDir, typeName string, opts QueryOptions) ([]*Entity, error) {
	entities, err := ListEntitiesByType(kgDir, typeName)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // an absent type is an empty result, not an error
		}
		return nil, err
	}
	sort.Slice(entities, func(i, j int) bool { return entities[i].ID < entities[j].ID })

	var results []*Entity
	matched := 0
	for _, e := range entities {
		if !MatchFilters(e, opts.Filters) {
			continue
		}
		if matched < opts.Offset {
			matched++
			continue
		}
		results = append(results, project(e, opts))
		matched++
		if opts.Limit > 0 && len(results) >= opts.Limit {
			break
		}
	}
	return results, nil
}

// Loader reads entities on demand and remembers what it has read.
//
// Traversal touches the same entity repeatedly and only ever reaches the part
// of the graph the relations lead to, so reading lazily costs the reachable
// set rather than the corpus. A full walk is Graph; this is for the queries
// that are not full walks.
type Loader struct {
	kgDir string
	cache map[string]*Entity
}

// NewLoader returns a Loader for a graph directory.
func NewLoader(kgDir string) *Loader {
	return &Loader{kgDir: kgDir, cache: make(map[string]*Entity)}
}

// Entity returns one entity, reading it the first time it is asked for. A
// missing entity is a cached miss, not an error: a dangling relation target is
// reported by validation (P3), never by a traversal blowing up.
func (l *Loader) Entity(id string) (*Entity, bool) {
	if e, ok := l.cache[id]; ok {
		return e, e != nil
	}
	e, err := ReadEntity(l.kgDir, id)
	if err != nil {
		l.cache[id] = nil
		return nil, false
	}
	l.cache[id] = e
	return e, true
}

// Relations returns the targets of one relation on one entity.
func (l *Loader) Relations(id, relation string) []string {
	e, ok := l.Entity(id)
	if !ok {
		return nil
	}
	return e.Relations[relation]
}

// EntityFile pairs an entity with the file it was read from.
//
// The two are not interchangeable: EntityPath derives a path from an id, and
// on a corpus where some file's declared id disagrees with where it sits
// (PRINCIPLES.md violation #3) those differ. Anything that walks the corpus and
// rewrites what it finds must write back to the file it read, or it edits one
// entity and clobbers another.
type EntityFile struct {
	Path   string
	Entity *Entity
}

// ListEntityFiles walks .kg/entities/ and returns every entity with its path,
// sorted by path so a whole-corpus pass is deterministic.
func ListEntityFiles(kgDir string) ([]EntityFile, error) {
	entitiesDir := filepath.Join(kgDir, "entities")
	var out []EntityFile

	err := filepath.WalkDir(entitiesDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) && path == entitiesDir {
				return nil
			}
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
		out = append(out, EntityFile{Path: path, Entity: &e})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// WriteEntityAt writes an entity to an explicit path rather than to the path
// its id implies. Use it when rewriting a file you have already read.
func WriteEntityAt(path string, e *Entity) error {
	journalNote(path)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating directory for %s: %w", path, err)
	}
	data, err := yaml.Marshal(e)
	if err != nil {
		return fmt.Errorf("marshaling entity %q: %w", e.ID, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
