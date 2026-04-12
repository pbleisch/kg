package store

import (
	"os"
	"path/filepath"
	"testing"
)

// setupKGDir creates a temp kgDir and writes the given entities as YAML files.
func setupKGDir(t *testing.T, entities []*Entity) string {
	t.Helper()
	dir := t.TempDir()
	for _, e := range entities {
		if err := WriteEntity(dir, e); err != nil {
			t.Fatalf("WriteEntity(%s): %v", e.ID, err)
		}
	}
	return dir
}

func TestSyncFreshEmpty(t *testing.T) {
	dir := t.TempDir()

	if err := Sync(dir); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if len(m.Files) != 0 {
		t.Errorf("expected empty manifest, got %d files", len(m.Files))
	}
}

func TestSyncFresh(t *testing.T) {
	entities := []*Entity{
		{ID: "person:alice", Type: "Person", Attributes: map[string]any{"name": "Alice"}},
		{ID: "project:atlas", Type: "Project", Attributes: map[string]any{"name": "Atlas"}},
	}
	dir := setupKGDir(t, entities)

	if err := Sync(dir); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	// Verify manifest has 2 entries.
	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if len(m.Files) != 2 {
		t.Errorf("manifest files = %d, want 2", len(m.Files))
	}

	// Verify entities are in the index.
	idx, err := OpenIndex(dir)
	if err != nil {
		t.Fatalf("OpenIndex: %v", err)
	}
	defer idx.Close()

	got, err := idx.GetEntity("person:alice")
	if err != nil {
		t.Fatalf("GetEntity(person:alice): %v", err)
	}
	if got.Type != "Person" {
		t.Errorf("type = %q, want Person", got.Type)
	}

	ids, err := idx.FindByType("Project")
	if err != nil {
		t.Fatalf("FindByType: %v", err)
	}
	if len(ids) != 1 || ids[0] != "project:atlas" {
		t.Errorf("FindByType(Project) = %v, want [project:atlas]", ids)
	}
}

func TestSyncAdded(t *testing.T) {
	entities := []*Entity{
		{ID: "person:alice", Type: "Person", Attributes: map[string]any{"name": "Alice"}},
	}
	dir := setupKGDir(t, entities)

	if err := Sync(dir); err != nil {
		t.Fatalf("Sync (initial): %v", err)
	}

	// Add a new entity.
	newEntity := &Entity{ID: "person:bob", Type: "Person", Attributes: map[string]any{"name": "Bob"}}
	if err := WriteEntity(dir, newEntity); err != nil {
		t.Fatalf("WriteEntity: %v", err)
	}

	if err := Sync(dir); err != nil {
		t.Fatalf("Sync (after add): %v", err)
	}

	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if len(m.Files) != 2 {
		t.Errorf("manifest files = %d, want 2", len(m.Files))
	}

	idx, err := OpenIndex(dir)
	if err != nil {
		t.Fatalf("OpenIndex: %v", err)
	}
	defer idx.Close()

	got, err := idx.GetEntity("person:bob")
	if err != nil {
		t.Fatalf("GetEntity(person:bob): %v", err)
	}
	if got.Attributes["name"] != "Bob" {
		t.Errorf("name = %v, want Bob", got.Attributes["name"])
	}
}

func TestSyncModified(t *testing.T) {
	entities := []*Entity{
		{ID: "person:alice", Type: "Person", Attributes: map[string]any{"name": "Alice"}},
	}
	dir := setupKGDir(t, entities)

	if err := Sync(dir); err != nil {
		t.Fatalf("Sync (initial): %v", err)
	}

	// Modify the entity.
	updated := &Entity{ID: "person:alice", Type: "Person", Attributes: map[string]any{"name": "Alice Chen"}}
	if err := WriteEntity(dir, updated); err != nil {
		t.Fatalf("WriteEntity: %v", err)
	}

	if err := Sync(dir); err != nil {
		t.Fatalf("Sync (after modify): %v", err)
	}

	idx, err := OpenIndex(dir)
	if err != nil {
		t.Fatalf("OpenIndex: %v", err)
	}
	defer idx.Close()

	got, err := idx.GetEntity("person:alice")
	if err != nil {
		t.Fatalf("GetEntity: %v", err)
	}
	if got.Attributes["name"] != "Alice Chen" {
		t.Errorf("name = %v, want Alice Chen", got.Attributes["name"])
	}
}

func TestSyncDeleted(t *testing.T) {
	entities := []*Entity{
		{ID: "person:alice", Type: "Person", Attributes: map[string]any{"name": "Alice"}},
		{ID: "person:bob", Type: "Person", Attributes: map[string]any{"name": "Bob"}},
	}
	dir := setupKGDir(t, entities)

	if err := Sync(dir); err != nil {
		t.Fatalf("Sync (initial): %v", err)
	}

	// Delete bob's YAML file.
	if err := DeleteEntity(dir, "person:bob"); err != nil {
		t.Fatalf("DeleteEntity: %v", err)
	}

	if err := Sync(dir); err != nil {
		t.Fatalf("Sync (after delete): %v", err)
	}

	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if len(m.Files) != 1 {
		t.Errorf("manifest files = %d, want 1", len(m.Files))
	}

	idx, err := OpenIndex(dir)
	if err != nil {
		t.Fatalf("OpenIndex: %v", err)
	}
	defer idx.Close()

	// Bob should be gone from the index.
	_, err = idx.GetEntity("person:bob")
	if err == nil {
		t.Error("expected error for deleted entity person:bob")
	}

	// Alice should still be there.
	got, err := idx.GetEntity("person:alice")
	if err != nil {
		t.Fatalf("GetEntity(person:alice): %v", err)
	}
	if got.Type != "Person" {
		t.Errorf("alice type = %q, want Person", got.Type)
	}
}

func TestRebuild(t *testing.T) {
	entities := []*Entity{
		{ID: "person:alice", Type: "Person", Attributes: map[string]any{"name": "Alice"}},
		{ID: "project:atlas", Type: "Project", Attributes: map[string]any{"name": "Atlas"}},
	}
	dir := setupKGDir(t, entities)

	// Do an initial sync to populate the index.
	if err := Sync(dir); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	// Verify the index file/dir exists.
	dbPath := filepath.Join(dir, ".cache", "ladybug")
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("expected index at %s: %v", dbPath, err)
	}

	// Rebuild from scratch.
	if err := Rebuild(dir); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	// Verify manifest is fresh.
	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if len(m.Files) != 2 {
		t.Errorf("manifest files = %d, want 2", len(m.Files))
	}

	// Verify index is correct.
	idx, err := OpenIndex(dir)
	if err != nil {
		t.Fatalf("OpenIndex: %v", err)
	}
	defer idx.Close()

	got, err := idx.GetEntity("person:alice")
	if err != nil {
		t.Fatalf("GetEntity(person:alice): %v", err)
	}
	if got.Attributes["name"] != "Alice" {
		t.Errorf("name = %v, want Alice", got.Attributes["name"])
	}

	ids, err := idx.FindByType("Project")
	if err != nil {
		t.Fatalf("FindByType: %v", err)
	}
	if len(ids) != 1 || ids[0] != "project:atlas" {
		t.Errorf("FindByType(Project) = %v, want [project:atlas]", ids)
	}
}
