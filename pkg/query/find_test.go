package query

import (
	"slices"
	"testing"

	"github.com/pbleisch/kg/pkg/store"
)

// findSetupKGDir creates a temp dir with entities for find tests.
func findSetupKGDir(t *testing.T, entities []*store.Entity) string {
	t.Helper()
	dir := t.TempDir()
	for _, e := range entities {
		if err := store.WriteEntity(dir, e); err != nil {
			t.Fatalf("WriteEntity(%s): %v", e.ID, err)
		}
	}
	return dir
}

var findTestEntities = []*store.Entity{
	{
		ID:   "person:alice",
		Type: "person",
		Attributes: map[string]any{
			"name":   "Alice",
			"role":   "Engineer",
			"salary": 120000,
		},
	},
	{
		ID:   "person:bob",
		Type: "person",
		Attributes: map[string]any{
			"name":   "Bob",
			"role":   "Designer",
			"salary": 95000,
		},
	},
	{
		ID:   "person:carol",
		Type: "person",
		Attributes: map[string]any{
			"name":   "Carol",
			"role":   "Senior Engineer",
			"salary": 150000,
		},
	},
	{
		ID:   "project:atlas",
		Type: "project",
		Attributes: map[string]any{
			"name": "Atlas",
		},
	},
}

func TestParseFilterEquality(t *testing.T) {
	f, err := ParseFilter("role=Engineer")
	if err != nil {
		t.Fatal(err)
	}
	if f.Attr != "role" || f.Op != "=" || f.Value != "Engineer" {
		t.Fatalf("got %+v", f)
	}
}

func TestParseFilterNotEqual(t *testing.T) {
	f, err := ParseFilter("role!=Engineer")
	if err != nil {
		t.Fatal(err)
	}
	if f.Attr != "role" || f.Op != "!=" || f.Value != "Engineer" {
		t.Fatalf("got %+v", f)
	}
}

func TestParseFilterContains(t *testing.T) {
	f, err := ParseFilter("role~Engineer")
	if err != nil {
		t.Fatal(err)
	}
	if f.Attr != "role" || f.Op != "~" || f.Value != "Engineer" {
		t.Fatalf("got %+v", f)
	}
}

func TestParseFilterContainsTildeEquals(t *testing.T) {
	f, err := ParseFilter("role~=Engineer")
	if err != nil {
		t.Fatal(err)
	}
	// ~= normalizes to ~
	if f.Attr != "role" || f.Op != "~" || f.Value != "Engineer" {
		t.Fatalf("got %+v, want Op=~ ", f)
	}
}

func TestParseFilterGreaterThan(t *testing.T) {
	f, err := ParseFilter("salary>100000")
	if err != nil {
		t.Fatal(err)
	}
	if f.Attr != "salary" || f.Op != ">" || f.Value != "100000" {
		t.Fatalf("got %+v", f)
	}
}

func TestParseFilterLessThan(t *testing.T) {
	f, err := ParseFilter("salary<100000")
	if err != nil {
		t.Fatal(err)
	}
	if f.Attr != "salary" || f.Op != "<" || f.Value != "100000" {
		t.Fatalf("got %+v", f)
	}
}

func TestParseFilterGreaterEqual(t *testing.T) {
	f, err := ParseFilter("salary>=100000")
	if err != nil {
		t.Fatal(err)
	}
	if f.Attr != "salary" || f.Op != ">=" || f.Value != "100000" {
		t.Fatalf("got %+v", f)
	}
}

func TestParseFilterLessEqual(t *testing.T) {
	f, err := ParseFilter("salary<=100000")
	if err != nil {
		t.Fatal(err)
	}
	if f.Attr != "salary" || f.Op != "<=" || f.Value != "100000" {
		t.Fatalf("got %+v", f)
	}
}

func TestParseFilterInvalid(t *testing.T) {
	_, err := ParseFilter("nooperator")
	if err == nil {
		t.Fatal("expected error for filter without operator")
	}
}

func TestFindByTypeOnly(t *testing.T) {
	dir := findSetupKGDir(t, findTestEntities)

	results, err := Find(dir, "person", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 persons, got %d", len(results))
	}
}

func TestFindWithEqualityFilter(t *testing.T) {
	dir := findSetupKGDir(t, findTestEntities)

	filters := []Filter{{Attr: "role", Op: "=", Value: "Engineer"}}
	results, err := Find(dir, "person", filters)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ID != "person:alice" {
		t.Fatalf("expected alice, got %s", results[0].ID)
	}
}

func TestFindWithContainsFilter(t *testing.T) {
	dir := findSetupKGDir(t, findTestEntities)

	filters := []Filter{{Attr: "role", Op: "~", Value: "engineer"}}
	results, err := Find(dir, "person", filters)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results (Alice+Carol), got %d", len(results))
	}
	ids := []string{results[0].ID, results[1].ID}
	slices.Sort(ids)
	if ids[0] != "person:alice" || ids[1] != "person:carol" {
		t.Fatalf("unexpected IDs: %v", ids)
	}
}

func TestFindWithNumericComparison(t *testing.T) {
	dir := findSetupKGDir(t, findTestEntities)

	filters := []Filter{{Attr: "salary", Op: ">=", Value: "100000"}}
	results, err := Find(dir, "person", filters)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results (Alice+Carol), got %d", len(results))
	}
	ids := []string{results[0].ID, results[1].ID}
	slices.Sort(ids)
	if ids[0] != "person:alice" || ids[1] != "person:carol" {
		t.Fatalf("unexpected IDs: %v", ids)
	}
}

func TestFindNoMatch(t *testing.T) {
	dir := findSetupKGDir(t, findTestEntities)

	filters := []Filter{{Attr: "role", Op: "=", Value: "CEO"}}
	results, err := Find(dir, "person", filters)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}
