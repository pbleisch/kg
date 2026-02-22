package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Manifest tracks which YAML entity files have been synced to the index.
type Manifest struct {
	Version int                  `json:"version"`
	SyncedAt time.Time           `json:"synced_at"`
	Files   map[string]FileEntry `json:"files"`
}

// FileEntry records the checksum and modification time of a synced file.
type FileEntry struct {
	Checksum string    `json:"checksum"`
	ModTime  time.Time `json:"mod_time"`
}

func manifestPath(kgDir string) string {
	return filepath.Join(kgDir, ".cache", "manifest.json")
}

// LoadManifest reads the manifest from disk. Returns an empty manifest if the
// file does not exist.
func LoadManifest(kgDir string) (*Manifest, error) {
	p := manifestPath(kgDir)
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return &Manifest{Version: 1, Files: make(map[string]FileEntry)}, nil
		}
		return nil, fmt.Errorf("reading manifest: %w", err)
	}

	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing manifest: %w", err)
	}
	if m.Files == nil {
		m.Files = make(map[string]FileEntry)
	}
	return &m, nil
}

// SaveManifest writes the manifest to disk, creating directories as needed.
func SaveManifest(kgDir string, m *Manifest) error {
	p := manifestPath(kgDir)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("creating cache dir: %w", err)
	}

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling manifest: %w", err)
	}

	if err := os.WriteFile(p, data, 0o644); err != nil {
		return fmt.Errorf("writing manifest: %w", err)
	}
	return nil
}

// fileChecksum computes the SHA-256 hex digest of a file.
func fileChecksum(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Sync performs an incremental sync from YAML entity files to the index.
// Only files that are new, modified, or deleted since the last sync are
// processed.
func Sync(kgDir string) error {
	idx, err := OpenIndex(kgDir)
	if err != nil {
		return fmt.Errorf("opening index: %w", err)
	}
	defer idx.Close()

	manifest, err := LoadManifest(kgDir)
	if err != nil {
		return fmt.Errorf("loading manifest: %w", err)
	}

	entitiesDir := filepath.Join(kgDir, "entities")
	currentFiles := make(map[string]struct{})

	// Walk all YAML files under entities/.
	err = filepath.WalkDir(entitiesDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) && path == entitiesDir {
				return nil // no entities dir yet
			}
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".yaml") {
			return nil
		}

		rel, err := filepath.Rel(kgDir, path)
		if err != nil {
			return fmt.Errorf("computing relative path: %w", err)
		}
		currentFiles[rel] = struct{}{}

		checksum, err := fileChecksum(path)
		if err != nil {
			return fmt.Errorf("checksumming %s: %w", rel, err)
		}

		prev, exists := manifest.Files[rel]
		if exists && prev.Checksum == checksum {
			return nil // unchanged
		}

		// Read the entity from the YAML file.
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", rel, err)
		}
		var e Entity
		if err := yaml.Unmarshal(data, &e); err != nil {
			return fmt.Errorf("parsing %s: %w", rel, err)
		}

		// For updates, remove old quads first.
		if exists {
			_ = idx.RemoveEntity(e.ID)
		}

		if err := idx.AddEntity(&e); err != nil {
			return fmt.Errorf("indexing %s: %w", rel, err)
		}

		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("stat %s: %w", rel, err)
		}

		manifest.Files[rel] = FileEntry{
			Checksum: checksum,
			ModTime:  info.ModTime(),
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("walking entities: %w", err)
	}

	// Remove entries for files that no longer exist on disk.
	for rel := range manifest.Files {
		if _, ok := currentFiles[rel]; !ok {
			// Derive entity ID from path to remove from index.
			if e := entityFromRelPath(kgDir, rel); e != nil {
				_ = idx.RemoveEntity(e.ID)
			}
			delete(manifest.Files, rel)
		}
	}

	manifest.SyncedAt = time.Now()
	if err := SaveManifest(kgDir, manifest); err != nil {
		return fmt.Errorf("saving manifest: %w", err)
	}
	return nil
}

// Rebuild clears the index and manifest, then re-indexes all YAML entities.
func Rebuild(kgDir string) error {
	// Remove old index files for both backends.
	boltPath := filepath.Join(kgDir, ".cache", "cayley.bolt")
	if err := os.Remove(boltPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing bolt index: %w", err)
	}
	ladybugDir := filepath.Join(kgDir, ".cache", "ladybug")
	if err := os.RemoveAll(ladybugDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing ladybug index: %w", err)
	}

	idx, err := OpenIndex(kgDir)
	if err != nil {
		return fmt.Errorf("opening index: %w", err)
	}
	defer idx.Close()

	entities, err := ListEntities(kgDir)
	if err != nil {
		if os.IsNotExist(err) {
			entities = nil
		} else {
			return fmt.Errorf("listing entities: %w", err)
		}
	}

	manifest := &Manifest{
		Version: 1,
		Files:   make(map[string]FileEntry),
	}

	for _, e := range entities {
		if err := idx.AddEntity(e); err != nil {
			return fmt.Errorf("indexing %s: %w", e.ID, err)
		}

		path := EntityPath(kgDir, e.ID)
		rel, err := filepath.Rel(kgDir, path)
		if err != nil {
			return fmt.Errorf("computing relative path: %w", err)
		}

		checksum, err := fileChecksum(path)
		if err != nil {
			return fmt.Errorf("checksumming %s: %w", e.ID, err)
		}

		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("stat %s: %w", e.ID, err)
		}

		manifest.Files[rel] = FileEntry{
			Checksum: checksum,
			ModTime:  info.ModTime(),
		}
	}

	manifest.SyncedAt = time.Now()
	if err := SaveManifest(kgDir, manifest); err != nil {
		return fmt.Errorf("saving manifest: %w", err)
	}
	return nil
}

// entityFromRelPath attempts to read an entity from a relative path within
// kgDir. Returns nil if the file cannot be read or parsed.
func entityFromRelPath(kgDir, rel string) *Entity {
	path := filepath.Join(kgDir, rel)
	data, err := os.ReadFile(path)
	if err != nil {
		// File is gone; try to reconstruct ID from path.
		// rel is like "entities/person/alice.yaml"
		parts := strings.Split(rel, string(filepath.Separator))
		if len(parts) >= 3 && parts[0] == "entities" {
			typeName := parts[1]
			localID := strings.TrimSuffix(parts[len(parts)-1], ".yaml")
			return &Entity{ID: typeName + ":" + localID}
		}
		return nil
	}
	var e Entity
	if err := yaml.Unmarshal(data, &e); err != nil {
		return nil
	}
	return &e
}
