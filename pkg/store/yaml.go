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

// ReadEntity reads and parses one entity file.
//
// It enforces the rules a single file can be judged by: the declared identity
// agrees with the location, the names are legal (P3, ADR-0028).  A file that
// breaks one comes back as a *LoadError, which is not an os.IsNotExist error —
// a caller asking "does this exist?" must not read a refusal as a miss.
func ReadEntity(kgDir, id string) (*Entity, error) {
	path := EntityPath(kgDir, id)
	if path == "" {
		return nil, fmt.Errorf("invalid entity ID %q", id)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading entity %q: %w", id, err)
	}

	e, lerr := decodeEntity(kgDir, path, data)
	if lerr != nil {
		return nil, lerr
	}
	return e, nil
}

// decodeEntity parses one entity file and applies every rule that needs only
// that file and its own path.
func decodeEntity(kgDir, path string, data []byte) (*Entity, error) {
	ident, err := IdentityAt(kgDir, path)
	if err != nil {
		return nil, err
	}
	var e Entity
	if err := yaml.Unmarshal(data, &e); err != nil {
		return nil, refuse(Structural{
			Code:    "malformed",
			ID:      ident.ID,
			Files:   []string{path},
			Message: fmt.Sprintf("is not parseable YAML: %v", err),
		})
	}
	if errs := CheckEntityFile(path, ident, &e); len(errs) > 0 {
		return nil, &LoadError{Errors: errs}
	}
	adopt(&e, ident)
	return &e, nil
}

// WriteEntity marshals an entity to YAML and writes it to the path its
// identity implies.
//
// It refuses what a load would refuse, so kg cannot write a file it will not
// read back: an entity whose declared `type:` disagrees with the type its id
// stores it under, or which carries an illegal attribute name, does not reach
// the disk (P3, ADR-0028).
func WriteEntity(kgDir string, e *Entity) error {
	path := EntityPath(kgDir, e.ID)
	if path == "" {
		return fmt.Errorf("invalid entity ID %q", e.ID)
	}
	ident, err := IdentityAt(kgDir, path)
	if err != nil {
		return err
	}
	if errs := CheckEntityFile(path, ident, e); len(errs) > 0 {
		return &LoadError{Errors: errs}
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
	return walkEntities(entitiesDir, kgDir)
}

// ListEntitiesByType lists entities of a single type.
func ListEntitiesByType(kgDir, typeName string) ([]*Entity, error) {
	typeDir := filepath.Join(kgDir, "entities", typeName)
	return walkEntities(typeDir, kgDir)
}

// walkEntities reads every .yaml file under dir and enforces P3's load half:
// each file's declared identity agrees with its location, its names are legal,
// and no identity is claimed twice.  It reports every offending file rather
// than the first, because a corpus is repaired in one pass or not at all.
func walkEntities(dir, kgDir string) ([]*Entity, error) {
	var entities []*Entity
	var errs []Structural
	byID := make(map[string][]string)

	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".yaml") {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			errs = append(errs, Structural{
				Code:    "unreadable",
				Files:   []string{path},
				Message: fmt.Sprintf("cannot be read: %v", err),
			})
			return nil
		}

		e, derr := decodeEntity(kgDir, path, data)
		if derr != nil {
			if ss, ok := LoadRefused(derr); ok {
				errs = append(errs, ss...)
				return nil
			}
			return derr
		}
		byID[e.ID] = append(byID[e.ID], path)
		entities = append(entities, e)
		return nil
	})
	if err != nil {
		return nil, err
	}
	errs = append(errs, Collisions(byID)...)
	if len(errs) > 0 {
		SortStructural(errs)
		return nil, &LoadError{Errors: errs}
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
	// refused collects the structural failures met on the way. A traversal
	// keeps going past one — it has partial results to return and the caller
	// decides — but it must not silently read a refusal as a miss, which is
	// the difference between "that entity is not there" and "that entity is
	// there twice".
	refused []Structural
}

// NewLoader returns a Loader for a graph directory.
func NewLoader(kgDir string) *Loader {
	return &Loader{kgDir: kgDir, cache: make(map[string]*Entity)}
}

// Entity returns one entity, reading it the first time it is asked for. A
// missing entity is a cached miss, not an error: a dangling relation target is
// reported, never a traversal blowing up (ADR-0028). A file that breaks a
// structural rule is a miss too, and is recorded — see [Loader.Err].
func (l *Loader) Entity(id string) (*Entity, bool) {
	if e, ok := l.cache[id]; ok {
		return e, e != nil
	}
	e, err := ReadEntity(l.kgDir, id)
	if err != nil {
		if ss, ok := LoadRefused(err); ok {
			l.refused = append(l.refused, ss...)
		}
		l.cache[id] = nil
		return nil, false
	}
	l.cache[id] = e
	return e, true
}

// Err reports the structural failures the traversal reached, as a *LoadError,
// or nil. Structure is enforced (P3): a read that touched a refused file
// answers with the refusal, not with a shorter path.
func (l *Loader) Err() error {
	if len(l.refused) == 0 {
		return nil
	}
	errs := append([]Structural(nil), l.refused...)
	SortStructural(errs)
	return &LoadError{Errors: errs}
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
//
// It deliberately enforces nothing. It is the read the repair uses, and the
// only one: a corpus that will not load must still be walkable by the tool
// that fixes it, and by the report that says what is wrong with it. Every
// other read refuses (ADR-0028). The entities it returns carry the `id:` the
// file declares, which on a corpus with disagreements is not the identity the
// path gives — use [IdentityAt] for that.
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

// RewriteEntityFile writes an entity back to a file that already exists,
// checking nothing.
//
// It is the write half of the repair pair — [ListEntityFiles] reads, this
// writes — and the only write that does not enforce P3. A pass that repairs
// one dimension of a corpus must be able to leave the others as it found
// them: the vector migration rewrites 27,000 files without their embeddings
// and has no business refusing the 265 whose id disagrees with their path.
//
// Anything that means to produce a well-formed file uses [WriteEntityAt].
func RewriteEntityFile(path string, e *Entity) error {
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

// WriteEntityAt writes an entity to an explicit path rather than to the path
// its id implies. Use it when rewriting a file you have already read.
//
// It refuses a write whose entity disagrees with the path it is going to, so
// the trap EntityFile documents is closed by the API rather than by a comment:
// a whole-corpus pass that writes an entity to the wrong file skips one entity
// and overwrites another, silently.
func WriteEntityAt(kgDir, path string, e *Entity) error {
	ident, err := IdentityAt(kgDir, path)
	if err != nil {
		return err
	}
	if errs := CheckEntityFile(path, ident, e); len(errs) > 0 {
		return &LoadError{Errors: errs}
	}
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
