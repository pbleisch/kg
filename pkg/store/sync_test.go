package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/pbleisch/kg/pkg/vector"
)

func TestSync_NoEmbedderIsALoudError(t *testing.T) {
	dir := t.TempDir()
	if err := WriteEntity(dir, &Entity{ID: "person:alice", Type: "person"}); err != nil {
		t.Fatal(err)
	}
	_, err := Sync(dir, SyncOptions{})
	var missing *ErrNoEmbedder
	if !errors.As(err, &missing) {
		t.Fatalf("Sync returned %v, want *ErrNoEmbedder — a sync that silently embeds nothing is the bug", err)
	}
}

func TestSync_NoEmbedIsQuiet(t *testing.T) {
	dir := t.TempDir()
	if err := WriteEntity(dir, &Entity{ID: "person:alice", Type: "person"}); err != nil {
		t.Fatal(err)
	}
	res, err := Sync(dir, SyncOptions{NoEmbed: true})
	if err != nil {
		t.Fatalf("Sync --no-embed: %v", err)
	}
	if res.Entities != 1 {
		t.Errorf("Entities = %d, want 1", res.Entities)
	}
}

// TestSync_RebuildsEverythingDerived is P2's first check made executable:
// delete every derived artifact, run one command, and the graph is whole again
// with nothing else touched.
func TestSync_RebuildsEverythingDerived(t *testing.T) {
	tmp := t.TempDir()
	cmd := writeMockEmbedScript(t, tmp)
	e := &Entity{ID: "person:alice", Type: "person", Attributes: map[string]any{"name": "Alice"}}
	dir := setupEmbedDir(t, []*Entity{e}, cmd, []string{"name"}, false)

	if _, err := Sync(dir, SyncOptions{}); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	before := entityBytes(t, dir)

	// Blow away every derived artifact.
	if err := os.RemoveAll(CacheDir(dir)); err != nil {
		t.Fatal(err)
	}
	res, err := Sync(dir, SyncOptions{})
	if err != nil {
		t.Fatalf("Sync after deleting the cache: %v", err)
	}
	if res.Embedded != 1 {
		t.Errorf("Embedded = %d rebuilding from nothing, want 1", res.Embedded)
	}
	if got := entityBytes(t, dir); got != before {
		t.Error("rebuilding derived data changed the entity files")
	}
	if _, err := os.Stat(vector.BinPath(CacheDir(dir))); err != nil {
		t.Errorf("sidecar was not rebuilt: %v", err)
	}
}

func TestSync_RebuildFlagDiscardsSidecar(t *testing.T) {
	tmp := t.TempDir()
	cmd := writeMockEmbedScript(t, tmp)
	e := &Entity{ID: "person:alice", Type: "person", Attributes: map[string]any{"name": "Alice"}}
	dir := setupEmbedDir(t, []*Entity{e}, cmd, []string{"name"}, false)

	if _, err := Sync(dir, SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	res, err := Sync(dir, SyncOptions{Rebuild: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Embedded != 1 || res.Carried != 0 {
		t.Errorf("Embedded=%d Carried=%d under --rebuild, want 1 and 0", res.Embedded, res.Carried)
	}
}

func TestSync_DryRunWritesNothing(t *testing.T) {
	tmp := t.TempDir()
	cmd := writeMockEmbedScript(t, tmp)
	e := &Entity{ID: "person:alice", Type: "person", Attributes: map[string]any{"name": "Alice"}}
	dir := setupEmbedDir(t, []*Entity{e}, cmd, []string{"name"}, false)

	if _, err := Sync(dir, SyncOptions{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(vector.BinPath(CacheDir(dir))); !os.IsNotExist(err) {
		t.Error("a dry run wrote a sidecar")
	}
}

func TestFileChecksum(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f")
	if err := os.WriteFile(p, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	a, err := FileChecksum(p)
	if err != nil {
		t.Fatal(err)
	}
	b, err := FileChecksum(p)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Error("FileChecksum is not deterministic")
	}
	if _, err := FileChecksum(filepath.Join(dir, "missing")); err == nil {
		t.Error("FileChecksum succeeded on a missing file")
	}
}

// entityBytes concatenates every entity file, so a test can assert that a
// derived-data operation left the source of truth alone.
func entityBytes(t *testing.T, kgDir string) string {
	t.Helper()
	var out string
	err := filepath.WalkDir(filepath.Join(kgDir, "entities"), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out += path + "\n" + string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
