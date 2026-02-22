//go:build ladybug

package store

import (
	"slices"
	"testing"
)

func tempLadybugIndex(t *testing.T) Indexer {
	t.Helper()
	dir := t.TempDir()
	idx, err := openLadybug(dir)
	if err != nil {
		t.Fatalf("openLadybug: %v", err)
	}
	t.Cleanup(func() { idx.Close() })
	return idx
}

func TestLadybugOpenClose(t *testing.T) {
	dir := t.TempDir()
	idx, err := openLadybug(dir)
	if err != nil {
		t.Fatalf("openLadybug: %v", err)
	}
	if err := idx.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Re-open the same store.
	idx2, err := openLadybug(dir)
	if err != nil {
		t.Fatalf("openLadybug (reopen): %v", err)
	}
	if err := idx2.Close(); err != nil {
		t.Fatalf("Close (reopen): %v", err)
	}
}

func TestLadybugAddGetEntity(t *testing.T) {
	idx := tempLadybugIndex(t)

	e := &Entity{
		ID:   "alice",
		Type: "person",
		Attributes: map[string]any{
			"name": "Alice",
			"age":  float64(30),
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

	knows := got.Relations["knows"]
	slices.Sort(knows)
	if len(knows) != 2 || knows[0] != "bob" || knows[1] != "carol" {
		t.Errorf("Relations[knows] = %v, want [bob carol]", knows)
	}
}

func TestLadybugGetEntityNotFound(t *testing.T) {
	idx := tempLadybugIndex(t)

	_, err := idx.GetEntity("nonexistent")
	if err == nil {
		t.Fatal("expected error for missing entity")
	}
}

func TestLadybugRemoveEntity(t *testing.T) {
	idx := tempLadybugIndex(t)

	e := &Entity{
		ID:         "bob",
		Type:       "person",
		Attributes: map[string]any{"name": "Bob"},
	}

	if err := idx.AddEntity(e); err != nil {
		t.Fatalf("AddEntity: %v", err)
	}

	if _, err := idx.GetEntity("bob"); err != nil {
		t.Fatalf("GetEntity before remove: %v", err)
	}

	if err := idx.RemoveEntity("bob"); err != nil {
		t.Fatalf("RemoveEntity: %v", err)
	}

	_, err := idx.GetEntity("bob")
	if err == nil {
		t.Fatal("expected error after RemoveEntity")
	}

	// Removing again should be a no-op.
	if err := idx.RemoveEntity("bob"); err != nil {
		t.Fatalf("RemoveEntity (second): %v", err)
	}
}

func TestLadybugFindByType(t *testing.T) {
	idx := tempLadybugIndex(t)

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

	empty, err := idx.FindByType("robot")
	if err != nil {
		t.Fatalf("FindByType: %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("FindByType(robot) = %v, want []", empty)
	}
}

func TestLadybugFindByRelationTarget(t *testing.T) {
	idx := tempLadybugIndex(t)

	alice := &Entity{
		ID:        "alice",
		Type:      "person",
		Attributes: map[string]any{"name": "Alice"},
		Relations: map[string][]string{"knows": {"bob"}},
	}
	bob := &Entity{
		ID:         "bob",
		Type:       "person",
		Attributes: map[string]any{"name": "Bob"},
	}

	if err := idx.AddEntity(alice); err != nil {
		t.Fatalf("AddEntity(alice): %v", err)
	}
	if err := idx.AddEntity(bob); err != nil {
		t.Fatalf("AddEntity(bob): %v", err)
	}

	ids, err := idx.FindByRelationTarget("knows", "bob")
	if err != nil {
		t.Fatalf("FindByRelationTarget: %v", err)
	}
	if len(ids) != 1 || ids[0] != "alice" {
		t.Errorf("FindByRelationTarget(knows, bob) = %v, want [alice]", ids)
	}

	empty, err := idx.FindByRelationTarget("knows", "carol")
	if err != nil {
		t.Fatalf("FindByRelationTarget: %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("FindByRelationTarget(knows, carol) = %v, want []", empty)
	}
}
