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
	"sync/atomic"
	"time"

	"gopkg.in/yaml.v3"
)

// batchMode when >0 causes Sync to be a no-op.  Call BeginBatch before a
// batch of writes and EndBatch (which runs a real Sync) when done.
var batchMode int32

// BeginBatch suppresses per-operation Sync calls.  Must be paired with EndBatch.
func BeginBatch() { atomic.AddInt32(&batchMode, 1) }

// EndBatch re-enables Sync and performs one final sync.
func EndBatch(kgDir string) error {
	atomic.AddInt32(&batchMode, -1)
	return Sync(kgDir)
}

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
	return filepath.Join(kgDir, ".cache", "manifest-ladybug.json")
}

// LoadManifest reads the manifest from disk. Returns an empty manifest if the
// file does not exist. If the main file is missing but a .tmp file exists
// (incomplete atomic write), the .tmp is cleaned up.
func LoadManifest(kgDir string) (*Manifest, error) {
	p := manifestPath(kgDir)
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			// Clean up incomplete atomic write if present.
			os.Remove(p + ".tmp")
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

// SaveManifest writes the manifest to disk atomically, creating directories
// as needed. Uses temp file → fsync → rename to prevent truncation on crash.
func SaveManifest(kgDir string, m *Manifest) error {
	p := manifestPath(kgDir)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("creating cache dir: %w", err)
	}

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling manifest: %w", err)
	}

	// Atomic write: temp file → fsync → rename
	tmp := p + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("creating temp manifest: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("writing temp manifest: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("syncing temp manifest: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("closing temp manifest: %w", err)
	}
	if err := os.Rename(tmp, p); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("renaming manifest: %w", err)
	}
	return nil
}

// FileChecksum computes the SHA-256 hex digest of a file.
func FileChecksum(path string) (string, error) {
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
// In batch mode (between BeginBatch/EndBatch) this is a no-op.
func Sync(kgDir string) error {
	if atomic.LoadInt32(&batchMode) > 0 {
		return nil
	}
	// If a daemon is keeping the index fresh, skip sync.
	if daemonRunning(kgDir) {
		return nil
	}
	_, err := SyncWithResult(kgDir)
	return err
}

// daemonRunning returns true if a daemon is serving the given kgDir.
// Uses a quick socket connectivity check.
func daemonRunning(kgDir string) bool {
	absDir, err := filepath.Abs(kgDir)
	if err != nil {
		return false
	}
	return DaemonAvailable(rpcSocketPath(absDir))
}

// rpcSocketPath returns the Unix socket path for the daemon RPC server.
// Falls back to a hashed path in the system temp dir when the default
// path would exceed Unix socket length limits.
func rpcSocketPath(kgDir string) string {
	p := filepath.Join(kgDir, ".cache", "daemon-rpc.sock")
	if len(p) < 100 {
		return p
	}
	h := sha256.Sum256([]byte(kgDir))
	return filepath.Join(os.TempDir(), fmt.Sprintf("kg-rpc-%x.sock", h[:8]))
}

// SyncResult collects statistics from a sync or rebuild operation.
type SyncResult struct {
	Synced   int      `json:"synced"`
	Skipped  int      `json:"skipped"`
	Removed  int      `json:"removed"`
	Embedded int      `json:"embedded,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// SyncWithResult performs an incremental sync and returns detailed statistics.
// Malformed entities are skipped with warnings rather than aborting the sync.
func SyncWithResult(kgDir string) (*SyncResult, error) {
	idx, err := OpenIndex(kgDir)
	if err != nil {
		return nil, fmt.Errorf("opening index: %w", err)
	}
	defer idx.Close()
	return SyncWithIndex(kgDir, idx)
}

// SyncWithIndex performs an incremental sync using an already-open Indexer.
// The caller owns the index lifetime — this function will not open or close it.
// Malformed entities are skipped with warnings rather than aborting the sync.
func SyncWithIndex(kgDir string, idx Indexer) (*SyncResult, error) {
	result := &SyncResult{}

	manifest, err := LoadManifest(kgDir)
	if err != nil {
		return nil, fmt.Errorf("loading manifest: %w", err)
	}

	entitiesDir := filepath.Join(kgDir, "entities")
	currentFiles := make(map[string]struct{})

	// Collect changed entities during walk, then batch-write to index.
	var toUpsert []*Entity
	type fileInfo struct {
		rel      string
		checksum string
		modTime  time.Time
	}
	var upsertInfo []fileInfo

	// Walk all YAML files under entities/.
	err = filepath.WalkDir(entitiesDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) && path == entitiesDir {
				return nil // no entities dir yet
			}
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s: %v", path, walkErr))
			result.Skipped++
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(path, ".yaml") {
			return nil
		}

		rel, err := filepath.Rel(kgDir, path)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s: relative path: %v", path, err))
			result.Skipped++
			return nil
		}
		currentFiles[rel] = struct{}{}

		// Fast path: skip unchanged files by ModTime before expensive checksum.
		prev, exists := manifest.Files[rel]
		if exists {
			info, err := d.Info()
			if err == nil && info.ModTime().Equal(prev.ModTime) {
				return nil // unchanged since last sync
			}
		}

		checksum, err := FileChecksum(path)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s: checksum: %v", rel, err))
			result.Skipped++
			return nil
		}

		if exists && prev.Checksum == checksum {
			return nil // unchanged
		}

		// Read the entity from the YAML file.
		data, err := os.ReadFile(path)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s: read: %v", rel, err))
			result.Skipped++
			return nil
		}
		var e Entity
		if err := yaml.Unmarshal(data, &e); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s: parse: %v", rel, err))
			result.Skipped++
			return nil
		}

		info, err := d.Info()
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s: stat: %v", rel, err))
			result.Skipped++
			return nil
		}

		toUpsert = append(toUpsert, &e)
		upsertInfo = append(upsertInfo, fileInfo{
			rel:      rel,
			checksum: checksum,
			modTime:  info.ModTime(),
		})
		result.Synced++
		return nil
	})
	if err != nil {
		return result, fmt.Errorf("walking entities: %w", err)
	}

	// Batch-write changed entities to index.
	if len(toUpsert) > 0 {
		if bi, ok := idx.(BatchIndexer); ok {
			if err := bi.UpsertEntities(toUpsert); err != nil {
				return result, fmt.Errorf("batch upsert: %w", err)
			}
		} else {
			for _, e := range toUpsert {
				if err := idx.UpsertEntity(e); err != nil {
					result.Warnings = append(result.Warnings, fmt.Sprintf("%s: index: %v", e.ID, err))
				}
			}
		}
		// Manifest update deferred until after embedding pass.
	}

	// Collect deleted files and batch-remove from index.
	var toRemoveIDs []string
	var toRemoveRels []string
	for rel := range manifest.Files {
		if _, ok := currentFiles[rel]; !ok {
			if e := entityFromRelPath(kgDir, rel); e != nil {
				toRemoveIDs = append(toRemoveIDs, e.ID)
			}
			toRemoveRels = append(toRemoveRels, rel)
			result.Removed++
		}
	}
	if len(toRemoveIDs) > 0 {
		if bi, ok := idx.(BatchIndexer); ok {
			_ = bi.RemoveEntities(toRemoveIDs)
		} else {
			for _, id := range toRemoveIDs {
				_ = idx.RemoveEntity(id)
			}
		}
		for _, rel := range toRemoveRels {
			delete(manifest.Files, rel)
		}
	}

	// Run embedding pass if configured (best-effort).
	embedded, failedEmbeds := syncEmbed(kgDir, false)
	result.Embedded = embedded

	// Update manifest for upserted entities, skipping those with embedding failures
	// so they will be re-processed on next sync.
	for i, fi := range upsertInfo {
		if _, failed := failedEmbeds[toUpsert[i].ID]; failed {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("%s: embedding failed, will retry on next sync", toUpsert[i].ID))
			continue
		}
		manifest.Files[fi.rel] = FileEntry{
			Checksum: fi.checksum,
			ModTime:  fi.modTime,
		}
	}

	manifest.SyncedAt = time.Now()
	if err := SaveManifest(kgDir, manifest); err != nil {
		return result, fmt.Errorf("saving manifest: %w", err)
	}

	return result, nil
}

// Rebuild clears the index and manifest, then re-indexes all YAML entities.
// Malformed entities are skipped. Use RebuildWithResult to inspect warnings.
func Rebuild(kgDir string) error {
	_, err := RebuildWithResult(kgDir)
	return err
}

// RebuildWithResult clears the index and re-indexes all entities, returning
// detailed statistics. Malformed entities are skipped with warnings.
func RebuildWithResult(kgDir string) (*SyncResult, error) {
	result := &SyncResult{}

	// Remove old index files.
	ladybugDir := filepath.Join(kgDir, ".cache", "ladybug")
	if err := os.RemoveAll(ladybugDir); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("removing ladybug index: %w", err)
	}

	// Open index directly — bypass daemon detection/autostart since we just
	// deleted the DB and need exclusive access for the rebuild.
	idx, err := openLadybug(kgDir)
	if err != nil {
		return nil, fmt.Errorf("opening index: %w", err)
	}
	defer idx.Close()

	manifest := &Manifest{
		Version: 1,
		Files:   make(map[string]FileEntry),
	}

	// Walk files directly instead of using ListEntities so we can skip
	// malformed files without aborting the entire rebuild.
	entitiesDir := filepath.Join(kgDir, "entities")

	var toAdd []*Entity
	type rebuildInfo struct {
		rel      string
		checksum string
		modTime  time.Time
	}
	var addInfo []rebuildInfo

	err = filepath.WalkDir(entitiesDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) && path == entitiesDir {
				return nil
			}
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s: %v", path, walkErr))
			result.Skipped++
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(path, ".yaml") {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s: read: %v", path, err))
			result.Skipped++
			return nil
		}

		var e Entity
		if err := yaml.Unmarshal(data, &e); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s: parse: %v", path, err))
			result.Skipped++
			return nil
		}

		rel, err := filepath.Rel(kgDir, path)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s: relative path: %v", path, err))
			result.Skipped++
			return nil
		}

		checksum, err := FileChecksum(path)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s: checksum: %v", path, err))
			result.Skipped++
			return nil
		}

		info, err := os.Stat(path)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s: stat: %v", path, err))
			result.Skipped++
			return nil
		}

		toAdd = append(toAdd, &e)
		addInfo = append(addInfo, rebuildInfo{
			rel:      rel,
			checksum: checksum,
			modTime:  info.ModTime(),
		})
		result.Synced++
		return nil
	})
	if err != nil {
		return result, fmt.Errorf("walking entities: %w", err)
	}

	// Batch-add all entities to the index.
	if len(toAdd) > 0 {
		if bi, ok := idx.(BatchIndexer); ok {
			if err := bi.UpsertEntities(toAdd); err != nil {
				return result, fmt.Errorf("batch add: %w", err)
			}
		} else {
			for _, e := range toAdd {
				if err := idx.AddEntity(e); err != nil {
					result.Warnings = append(result.Warnings, fmt.Sprintf("%s: index: %v", e.ID, err))
				}
			}
		}
		for _, ri := range addInfo {
			manifest.Files[ri.rel] = FileEntry{
				Checksum: ri.checksum,
				ModTime:  ri.modTime,
			}
		}
	}

	manifest.SyncedAt = time.Now()
	if err := SaveManifest(kgDir, manifest); err != nil {
		return result, fmt.Errorf("saving manifest: %w", err)
	}

	// Run embedding pass if configured — rebuild embeddings too.
	embedded, _ := syncEmbed(kgDir, true)
	result.Embedded = embedded

	return result, nil
}

// syncEmbed runs an embed pass as part of sync. Returns count of entities
// embedded and a set of entity IDs whose embeddings failed.
func syncEmbed(kgDir string, rebuild bool) (int, map[string]struct{}) {
	failed := make(map[string]struct{})
	cfg, err := LoadConfig(kgDir)
	if err != nil || cfg.Embedding == nil || cfg.Embedding.Command == "" {
		return 0, failed
	}
	embedResult, err := Embed(kgDir, rebuild, false)
	if embedResult != nil {
		for _, id := range embedResult.FailedEntities {
			failed[id] = struct{}{}
		}
	}
	if err != nil || embedResult == nil {
		return 0, failed
	}
	return embedResult.Embedded, failed
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
