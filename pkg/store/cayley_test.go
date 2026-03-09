package store

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func tempIndex(t *testing.T) Indexer {
	t.Helper()
	dir := t.TempDir()
	idx, err := OpenIndex(dir)
	if err != nil {
		t.Fatalf("OpenIndex: %v", err)
	}
	t.Cleanup(func() { idx.Close() })
	return idx
}

func TestOpenClose(t *testing.T) {
	dir := t.TempDir()
	idx, err := OpenIndex(dir)
	if err != nil {
		t.Fatalf("OpenIndex: %v", err)
	}
	if err := idx.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Re-open the same store.
	idx2, err := OpenIndex(dir)
	if err != nil {
		t.Fatalf("OpenIndex (reopen): %v", err)
	}
	if err := idx2.Close(); err != nil {
		t.Fatalf("Close (reopen): %v", err)
	}
}

func TestAddGetEntity(t *testing.T) {
	idx := tempIndex(t)

	e := &Entity{
		ID:   "alice",
		Type: "person",
		Attributes: map[string]any{
			"name": "Alice",
			"age":  "30",
		},
		Relations: map[string][]string{
			"knows": {"bob", "carol"},
		},
	}

	if err := idx.AddEntity(e); err != nil {
		t.Fatalf("AddEntity: %v", err)
	}

	got, err := idx.GetEntity("alice")
	if err != nil {
		t.Fatalf("GetEntity: %v", err)
	}

	if got.ID != "alice" {
		t.Errorf("ID = %q, want %q", got.ID, "alice")
	}
	if got.Type != "person" {
		t.Errorf("Type = %q, want %q", got.Type, "person")
	}
	if got.Attributes["name"] != "Alice" {
		t.Errorf("Attributes[name] = %v, want %q", got.Attributes["name"], "Alice")
	}
	if got.Attributes["age"] != "30" {
		t.Errorf("Attributes[age] = %v, want %q", got.Attributes["age"], "30")
	}

	knows := got.Relations["knows"]
	slices.Sort(knows)
	if len(knows) != 2 || knows[0] != "bob" || knows[1] != "carol" {
		t.Errorf("Relations[knows] = %v, want [bob carol]", knows)
	}
}

func TestGetEntityNotFound(t *testing.T) {
	idx := tempIndex(t)

	_, err := idx.GetEntity("nonexistent")
	if err == nil {
		t.Fatal("expected error for missing entity")
	}
}

func TestRemoveEntity(t *testing.T) {
	idx := tempIndex(t)

	e := &Entity{
		ID:         "bob",
		Type:       "person",
		Attributes: map[string]any{"name": "Bob"},
	}

	if err := idx.AddEntity(e); err != nil {
		t.Fatalf("AddEntity: %v", err)
	}

	// Verify it exists.
	if _, err := idx.GetEntity("bob"); err != nil {
		t.Fatalf("GetEntity before remove: %v", err)
	}

	if err := idx.RemoveEntity("bob"); err != nil {
		t.Fatalf("RemoveEntity: %v", err)
	}

	// Should be gone now.
	_, err := idx.GetEntity("bob")
	if err == nil {
		t.Fatal("expected error after RemoveEntity")
	}

	// Removing again should be a no-op.
	if err := idx.RemoveEntity("bob"); err != nil {
		t.Fatalf("RemoveEntity (second): %v", err)
	}
}

func TestFindByType(t *testing.T) {
	idx := tempIndex(t)

	entities := []*Entity{
		{ID: "alice", Type: "person", Attributes: map[string]any{}},
		{ID: "bob", Type: "person", Attributes: map[string]any{}},
		{ID: "acme", Type: "company", Attributes: map[string]any{}},
	}
	for _, e := range entities {
		if err := idx.AddEntity(e); err != nil {
			t.Fatalf("AddEntity(%s): %v", e.ID, err)
		}
	}

	people, err := idx.FindByType("person")
	if err != nil {
		t.Fatalf("FindByType: %v", err)
	}
	slices.Sort(people)
	if len(people) != 2 || people[0] != "alice" || people[1] != "bob" {
		t.Errorf("FindByType(person) = %v, want [alice bob]", people)
	}

	companies, err := idx.FindByType("company")
	if err != nil {
		t.Fatalf("FindByType: %v", err)
	}
	if len(companies) != 1 || companies[0] != "acme" {
		t.Errorf("FindByType(company) = %v, want [acme]", companies)
	}

	// Non-existent type.
	empty, err := idx.FindByType("robot")
	if err != nil {
		t.Fatalf("FindByType: %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("FindByType(robot) = %v, want []", empty)
	}
}

func TestCacheDirCreated(t *testing.T) {
	dir := t.TempDir()
	idx, err := OpenIndex(dir)
	if err != nil {
		t.Fatalf("OpenIndex: %v", err)
	}
	defer idx.Close()

	if ladybugAvailable {
		// With ladybug, the database is a directory created by LadybugDB.
		dbPath := filepath.Join(dir, ".cache", "ladybug")
		if _, err := os.Stat(dbPath); err != nil {
			t.Fatalf("ladybug db dir not created: %v", err)
		}
	} else {
		dbPath := dir + "/.cache/cayley.bolt"
		info, err := os.Stat(dbPath)
		if err != nil {
			t.Fatalf("db file not created: %v", err)
		}
		if info.IsDir() {
			t.Fatal("db path should be a file, not a directory")
		}
	}
}
