package store

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTestEntity(t *testing.T, kgDir, typeName, fileName, content string) string {
	t.Helper()
	dir := filepath.Join(kgDir, "entities", typeName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("creating dir: %v", err)
	}
	path := filepath.Join(dir, fileName)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing file: %v", err)
	}
	return path
}

func findFix(result *NormalizeResult, field string) *Fix {
	for i := range result.Fixes {
		if result.Fixes[i].Field == field {
			return &result.Fixes[i]
		}
	}
	return nil
}

func repair(t *testing.T, kgDir, path string) *NormalizeResult {
	t.Helper()
	result, err := NormalizeEntityFile(kgDir, path, RepairAll())
	if err != nil {
		t.Fatalf("NormalizeEntityFile: %v", err)
	}
	return result
}

func TestNormalize_AlreadyNormalized(t *testing.T) {
	kgDir := t.TempDir()
	path := writeTestEntity(t, kgDir, "person", "alice.yaml", `id: person:alice
type: person
attributes:
  name: Alice
relations:
  knows:
    - person:bob
`)

	result := repair(t, kgDir, path)
	if len(result.Fixes) != 0 {
		t.Errorf("expected no fixes, got %d: %v", len(result.Fixes), result.Fixes)
	}
}

func TestNormalize_LinksToRelations(t *testing.T) {
	kgDir := t.TempDir()
	path := writeTestEntity(t, kgDir, "person", "alice.yaml", `id: person:alice
type: person
attributes:
  name: Alice
links:
  knows:
    - person:bob
`)

	result := repair(t, kgDir, path)
	if f := findFix(result, "links"); f == nil {
		t.Error("expected links→relations fix")
	}
	e, err := ReadEntity(kgDir, "person:alice")
	if err != nil {
		t.Fatalf("ReadEntity: %v", err)
	}
	if len(e.Relations["knows"]) != 1 {
		t.Errorf("Relations = %v, want knows:[person:bob]", e.Relations)
	}
}

// The path is the identity (ADR-0029): a file at entities/person/alice.yaml is
// person:alice whatever its id line says, and the repair corrects the line.
func TestNormalize_IDFollowsPath(t *testing.T) {
	kgDir := t.TempDir()
	path := writeTestEntity(t, kgDir, "person", "alice.yaml", `id: Person:alice
type: Person
attributes:
  name: Alice
`)

	result := repair(t, kgDir, path)
	if f := findFix(result, "id"); f == nil {
		t.Error("expected an id fix")
	}
	if f := findFix(result, "type"); f == nil {
		t.Error("expected a type fix")
	}

	e, err := ReadEntity(kgDir, "person:alice")
	if err != nil {
		t.Fatalf("ReadEntity: %v", err)
	}
	if e.ID != "person:alice" || e.Type != "person" {
		t.Errorf("got %s/%s, want person:alice/person", e.ID, e.Type)
	}
}

// A file whose id names a *different* directory is corrected in place. The old
// behaviour moved it to EntityPath(id), which on the real corpus overwrites the
// file already sitting there and loses the entity that was in this one.
func TestNormalize_NeverMovesAFile(t *testing.T) {
	kgDir := t.TempDir()
	occupant := writeTestEntity(t, kgDir, "person", "alice.yaml", `id: person:alice
type: person
attributes:
  name: The one that was already there
`)
	stray := writeTestEntity(t, kgDir, "people", "alice.yaml", `id: person:alice
type: person
attributes:
  name: The one with the stale id
`)

	repair(t, kgDir, stray)

	if _, err := os.Stat(stray); err != nil {
		t.Errorf("the repaired file was moved or removed: %v", err)
	}
	e, err := ReadEntity(kgDir, "person:alice")
	if err != nil {
		t.Fatalf("ReadEntity: %v", err)
	}
	if e.Attributes["name"] != "The one that was already there" {
		t.Errorf("person:alice was overwritten: %v", e.Attributes["name"])
	}
	if _, err := os.Stat(occupant); err != nil {
		t.Errorf("the occupant lost its file: %v", err)
	}
	moved, err := ReadEntity(kgDir, "people:alice")
	if err != nil {
		t.Fatalf("reading the repaired file by its real identity: %v", err)
	}
	if moved.Attributes["name"] != "The one with the stale id" {
		t.Errorf("people:alice = %v", moved.Attributes["name"])
	}
}

func TestNormalize_MissingIDAndType(t *testing.T) {
	kgDir := t.TempDir()
	path := writeTestEntity(t, kgDir, "person", "alice.yaml", `attributes:
  name: Alice
`)

	result := repair(t, kgDir, path)
	if f := findFix(result, "id"); f == nil {
		t.Error("expected id fix")
	}
	if f := findFix(result, "type"); f == nil {
		t.Error("expected type fix")
	}
	if result.EntityID != "person:alice" {
		t.Errorf("EntityID = %s, want person:alice", result.EntityID)
	}
}

// An attribute name a producer invented by splitting a command line on '=' is
// dropped, with the value in the message so it is not lost silently.
func TestNormalize_DropsIllegalAttributes(t *testing.T) {
	kgDir := t.TempDir()
	path := writeTestEntity(t, kgDir, "person", "alice.yaml", `id: person:alice
type: person
attributes:
  name: Alice
  --dir: .kg
`)

	result := repair(t, kgDir, path)
	f := findFix(result, "attributes.--dir")
	if f == nil {
		t.Fatalf("expected the --dir attribute to be dropped, got %v", result.Fixes)
	}
	if want := `dropped illegal attribute "--dir" (was .kg)`; f.Message != want {
		t.Errorf("message = %q, want %q", f.Message, want)
	}

	e, err := ReadEntity(kgDir, "person:alice")
	if err != nil {
		t.Fatalf("ReadEntity: %v", err)
	}
	if _, still := e.Attributes["--dir"]; still {
		t.Error("--dir survived the repair")
	}
	if e.Attributes["name"] != "Alice" {
		t.Errorf("name = %v, want Alice", e.Attributes["name"])
	}
}

func TestNormalize_LeavesLegalAttributesAlone(t *testing.T) {
	kgDir := t.TempDir()
	path := writeTestEntity(t, kgDir, "person", "alice.yaml", `id: Person:alice
type: person
attributes:
  name: Alice
  age: 30
  active: true
`)

	repair(t, kgDir, path)
	e, err := ReadEntity(kgDir, "person:alice")
	if err != nil {
		t.Fatalf("ReadEntity: %v", err)
	}
	if e.Attributes["name"] != "Alice" || e.Attributes["age"] != 30 || e.Attributes["active"] != true {
		t.Errorf("attributes = %v", e.Attributes)
	}
}

// Relation *targets* are identities, and a repair does not guess at one. This
// used to snake_case them, which silently repoints an edge.
func TestNormalize_LeavesRelationTargetsAlone(t *testing.T) {
	kgDir := t.TempDir()
	path := writeTestEntity(t, kgDir, "person", "alice.yaml", `id: person:alice
type: person
relations:
  knowsWell:
    - Person:bob
`)

	result := repair(t, kgDir, path)
	if f := findFix(result, "relations.knowsWell"); f == nil {
		t.Error("expected the relation key to be normalized")
	}

	e, err := ReadEntity(kgDir, "person:alice")
	if err != nil {
		t.Fatalf("ReadEntity: %v", err)
	}
	got := e.Relations["knows_well"]
	if len(got) != 1 || got[0] != "Person:bob" {
		t.Errorf("knows_well = %v, want [Person:bob] untouched", got)
	}
}

func TestNormalize_DropsDanglingOnlyWhenAsked(t *testing.T) {
	kgDir := t.TempDir()
	path := writeTestEntity(t, kgDir, "person", "alice.yaml", `id: person:alice
type: person
relations:
  knows:
    - person:bob
    - person:nobody
`)

	if r := repair(t, kgDir, path); len(r.Fixes) != 0 {
		t.Errorf("a default repair touched a dangling target: %v", r.Fixes)
	}

	opts := RepairAll()
	opts.Dangling = true
	opts.Known = map[string]bool{"person:alice": true, "person:bob": true}
	result, err := NormalizeEntityFile(kgDir, path, opts)
	if err != nil {
		t.Fatalf("NormalizeEntityFile: %v", err)
	}
	if len(result.Fixes) != 1 {
		t.Fatalf("Fixes = %v, want one dropped target", result.Fixes)
	}

	e, err := ReadEntity(kgDir, "person:alice")
	if err != nil {
		t.Fatalf("ReadEntity: %v", err)
	}
	if got := e.Relations["knows"]; len(got) != 1 || got[0] != "person:bob" {
		t.Errorf("knows = %v, want [person:bob]", got)
	}
}

func TestNormalize_DryRunWritesNothing(t *testing.T) {
	kgDir := t.TempDir()
	path := writeTestEntity(t, kgDir, "person", "alice.yaml", `id: Person:alice
type: person
`)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	opts := RepairAll()
	opts.DryRun = true
	result, err := NormalizeEntityFile(kgDir, path, opts)
	if err != nil {
		t.Fatalf("NormalizeEntityFile: %v", err)
	}
	if len(result.Fixes) == 0 {
		t.Error("a dry run should still report the fix it would make")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("a dry run rewrote the file")
	}
}

func TestNormalize_BothLinksAndRelations(t *testing.T) {
	kgDir := t.TempDir()
	path := writeTestEntity(t, kgDir, "person", "alice.yaml", `id: person:alice
type: person
relations:
  knows:
    - person:bob
links:
  authored:
    - book:foo
`)

	result := repair(t, kgDir, path)
	if f := findFix(result, "links"); f == nil {
		t.Error("expected links merge fix")
	}

	e, err := ReadEntity(kgDir, "person:alice")
	if err != nil {
		t.Fatalf("ReadEntity: %v", err)
	}
	if len(e.Relations["knows"]) != 1 {
		t.Errorf("knows = %v", e.Relations["knows"])
	}
	if len(e.Relations["authored"]) != 1 {
		t.Errorf("authored = %v", e.Relations["authored"])
	}
}

// A file kg would not have written cannot be repaired by editing it: the
// filename itself is wrong, and choosing which of two encodings survives is a
// decision kg does not make.
func TestNormalize_ReportsUnfixablePath(t *testing.T) {
	kgDir := t.TempDir()
	path := writeTestEntity(t, kgDir, "source", "mäyrä.yaml", `id: source:x
type: source
`)

	result, err := NormalizeEntityFile(kgDir, path, RepairAll())
	if err != nil {
		t.Fatalf("NormalizeEntityFile: %v", err)
	}
	if len(result.Unfixable) != 1 || result.Unfixable[0].Code != "non_canonical_path" {
		t.Fatalf("Unfixable = %+v, want one non_canonical_path", result.Unfixable)
	}
	if len(result.Fixes) != 0 {
		t.Errorf("Fixes = %v, want none", result.Fixes)
	}
}
