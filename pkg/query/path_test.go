package query

import (
	"sort"
	"testing"

	"github.com/pbleisch/kg/pkg/store"
)

// pathTestKGDir creates a temp dir, writes entities, and syncs the index.
func pathTestKGDir(t *testing.T, entities []*store.Entity) string {
	t.Helper()
	dir := t.TempDir()
	for _, e := range entities {
		if err := store.WriteEntity(dir, e); err != nil {
			t.Fatalf("WriteEntity(%s): %v", e.ID, err)
		}
	}
	return dir
}

func TestPathSingleStep(t *testing.T) {
	dir := pathTestKGDir(t, []*store.Entity{
		{ID: "person:alice", Type: "person", Relations: map[string][]string{
			"worksOn": {"project:atlas"},
		}},
		{ID: "project:atlas", Type: "project"},
	})

	got, err := Path(dir, "person:alice", []PathStep{
		{Relation: "worksOn"},
	})
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	want := []string{"project:atlas"}
	if !strSliceEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestPathMultiStep(t *testing.T) {
	dir := pathTestKGDir(t, []*store.Entity{
		{ID: "person:alice", Type: "person", Relations: map[string][]string{
			"reportsTo": {"person:bob"},
		}},
		{ID: "person:bob", Type: "person", Relations: map[string][]string{
			"worksOn": {"project:atlas"},
		}},
		{ID: "project:atlas", Type: "project"},
	})

	got, err := Path(dir, "person:alice", []PathStep{
		{Relation: "reportsTo"},
		{Relation: "worksOn"},
	})
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	want := []string{"project:atlas"}
	if !strSliceEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestPathRecursiveWithCycle(t *testing.T) {
	// A -> B -> C -> A (cycle)
	dir := pathTestKGDir(t, []*store.Entity{
		{ID: "person:a", Type: "person", Relations: map[string][]string{
			"knows": {"person:b"},
		}},
		{ID: "person:b", Type: "person", Relations: map[string][]string{
			"knows": {"person:c"},
		}},
		{ID: "person:c", Type: "person", Relations: map[string][]string{
			"knows": {"person:a"},
		}},
	})

	got, err := Path(dir, "person:a", []PathStep{
		{Relation: "knows", Recursive: true},
	})
	if err != nil {
		t.Fatalf("Path: %v", err)
	}

	// Should find b and c (not a again, since a is the seed/visited)
	want := []string{"person:b", "person:c"}
	sort.Strings(got)
	if !strSliceEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestPathRecursiveChain(t *testing.T) {
	// A -> B -> C -> D (no cycle, linear chain)
	dir := pathTestKGDir(t, []*store.Entity{
		{ID: "person:a", Type: "person", Relations: map[string][]string{
			"reportsTo": {"person:b"},
		}},
		{ID: "person:b", Type: "person", Relations: map[string][]string{
			"reportsTo": {"person:c"},
		}},
		{ID: "person:c", Type: "person", Relations: map[string][]string{
			"reportsTo": {"person:d"},
		}},
		{ID: "person:d", Type: "person"},
	})

	got, err := Path(dir, "person:a", []PathStep{
		{Relation: "reportsTo", Recursive: true},
	})
	if err != nil {
		t.Fatalf("Path: %v", err)
	}

	want := []string{"person:b", "person:c", "person:d"}
	sort.Strings(got)
	if !strSliceEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestPathVerbose(t *testing.T) {
	dir := pathTestKGDir(t, []*store.Entity{
		{ID: "person:alice", Type: "person", Relations: map[string][]string{
			"reportsTo": {"person:bob"},
		}},
		{ID: "person:bob", Type: "person", Relations: map[string][]string{
			"worksOn": {"project:atlas"},
		}},
		{ID: "project:atlas", Type: "project"},
	})

	results, err := PathVerbose(dir, "person:alice", []PathStep{
		{Relation: "reportsTo"},
		{Relation: "worksOn"},
	})
	if err != nil {
		t.Fatalf("PathVerbose: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}

	// First result: depth 1, via reportsTo, person:bob
	r0 := results[0]
	if r0.Depth != 1 || r0.Via != "reportsTo" || r0.ID != "person:bob" {
		t.Errorf("results[0] = %+v, want {1, reportsTo, person:bob}", r0)
	}

	// Second result: depth 2, via worksOn, project:atlas
	r1 := results[1]
	if r1.Depth != 2 || r1.Via != "worksOn" || r1.ID != "project:atlas" {
		t.Errorf("results[1] = %+v, want {2, worksOn, project:atlas}", r1)
	}
}

func TestPathVerboseRecursive(t *testing.T) {
	dir := pathTestKGDir(t, []*store.Entity{
		{ID: "person:a", Type: "person", Relations: map[string][]string{
			"reportsTo": {"person:b"},
		}},
		{ID: "person:b", Type: "person", Relations: map[string][]string{
			"reportsTo": {"person:c"},
		}},
		{ID: "person:c", Type: "person"},
	})

	results, err := PathVerbose(dir, "person:a", []PathStep{
		{Relation: "reportsTo", Recursive: true},
	})
	if err != nil {
		t.Fatalf("PathVerbose: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}

	// b found at depth 1, c found at depth 2
	if results[0].Depth != 1 || results[0].ID != "person:b" {
		t.Errorf("results[0] = %+v, want {1, reportsTo, person:b}", results[0])
	}
	if results[1].Depth != 2 || results[1].ID != "person:c" {
		t.Errorf("results[1] = %+v, want {2, reportsTo, person:c}", results[1])
	}
}

func TestPathNoRelation(t *testing.T) {
	dir := pathTestKGDir(t, []*store.Entity{
		{ID: "person:alice", Type: "person"},
	})

	got, err := Path(dir, "person:alice", []PathStep{
		{Relation: "worksOn"},
	})
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

func strSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
