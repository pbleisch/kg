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

func TestNormalize_AlreadyNormalized(t *testing.T) {
	kgDir := t.TempDir()
	path := writeTestEntity(t, kgDir, "person", "alice.yaml", `id: person:alice
type: Person
attributes:
  name: Alice
relations:
  knows:
    - person:bob
`)

	result, err := NormalizeEntityFile(kgDir, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Fixes) != 0 {
		t.Errorf("expected no fixes, got %d: %v", len(result.Fixes), result.Fixes)
	}
}

func TestNormalize_LinksToRelations(t *testing.T) {
	kgDir := t.TempDir()
	path := writeTestEntity(t, kgDir, "person", "alice.yaml", `id: person:alice
type: Person
attributes:
  name: Alice
links:
  knows:
    - person:bob
`)

	result, err := NormalizeEntityFile(kgDir, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if f := findFix(result, "links"); f == nil {
		t.Error("expected links→relations fix")
	}

	e, err := ReadEntity(kgDir, "person:alice")
	if err != nil {
		t.Fatalf("reading normalized entity: %v", err)
	}
	if len(e.Relations["knows"]) != 1 || e.Relations["knows"][0] != "person:bob" {
		t.Errorf("expected relations.knows=[person:bob], got %v", e.Relations)
	}
}

func TestNormalize_IDUppercaseType(t *testing.T) {
	kgDir := t.TempDir()
	path := writeTestEntity(t, kgDir, "person", "alice.yaml", `id: Person:alice
type: Person
attributes:
  name: Alice
`)

	result, err := NormalizeEntityFile(kgDir, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if f := findFix(result, "id"); f == nil {
		t.Error("expected id normalization fix")
	}

	e, err := ReadEntity(kgDir, "person:alice")
	if err != nil {
		t.Fatalf("reading normalized entity: %v", err)
	}
	if e.ID != "person:alice" {
		t.Errorf("expected id person:alice, got %s", e.ID)
	}
}

func TestNormalize_MissingID(t *testing.T) {
	kgDir := t.TempDir()
	path := writeTestEntity(t, kgDir, "person", "alice.yaml", `type: Person
attributes:
  name: Alice
`)

	result, err := NormalizeEntityFile(kgDir, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if f := findFix(result, "id"); f == nil {
		t.Error("expected id fix")
	}
	if result.EntityID != "person:alice" {
		t.Errorf("expected entity_id person:alice, got %s", result.EntityID)
	}
}

func TestNormalize_BareID(t *testing.T) {
	kgDir := t.TempDir()
	path := writeTestEntity(t, kgDir, "person", "alice.yaml", `id: alice
type: Person
attributes:
  name: Alice
`)

	result, err := NormalizeEntityFile(kgDir, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if f := findFix(result, "id"); f == nil {
		t.Error("expected id fix for bare ID")
	}
	if result.EntityID != "person:alice" {
		t.Errorf("expected entity_id person:alice, got %s", result.EntityID)
	}
}

func TestNormalize_TypeToLowerPascal(t *testing.T) {
	kgDir := t.TempDir()
	path := writeTestEntity(t, kgDir, "person", "alice.yaml", `id: person:alice
type: PERSON
attributes:
  name: Alice
`)

	result, err := NormalizeEntityFile(kgDir, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if f := findFix(result, "type"); f == nil {
		t.Error("expected type normalization fix")
	}

	e, err := ReadEntity(kgDir, "person:alice")
	if err != nil {
		t.Fatalf("reading normalized entity: %v", err)
	}
	if e.Type != "Person" {
		t.Errorf("expected type Person, got %s", e.Type)
	}
}

func TestNormalize_MissingType(t *testing.T) {
	kgDir := t.TempDir()
	path := writeTestEntity(t, kgDir, "person", "alice.yaml", `id: person:alice
attributes:
  name: Alice
`)

	result, err := NormalizeEntityFile(kgDir, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if f := findFix(result, "type"); f == nil {
		t.Error("expected type fix")
	}

	e, err := ReadEntity(kgDir, "person:alice")
	if err != nil {
		t.Fatalf("reading normalized entity: %v", err)
	}
	if e.Type != "Person" {
		t.Errorf("expected type Person, got %s", e.Type)
	}
}

func TestNormalize_RelationTargetCasing(t *testing.T) {
	kgDir := t.TempDir()
	path := writeTestEntity(t, kgDir, "person", "alice.yaml", `id: person:alice
type: Person
relations:
  knows:
    - Person:bob
    - person:carol
`)

	result, err := NormalizeEntityFile(kgDir, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if f := findFix(result, "relations.knows"); f == nil {
		t.Error("expected relation target fix")
	}

	e, err := ReadEntity(kgDir, "person:alice")
	if err != nil {
		t.Fatalf("reading normalized entity: %v", err)
	}

	targets := e.Relations["knows"]
	if len(targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(targets))
	}
	for _, target := range targets {
		tp, _, _ := ParseEntityID(target)
		if tp != "person" {
			t.Errorf("expected lowercase type in target, got %s", target)
		}
	}
}

func TestNormalize_FileMove(t *testing.T) {
	kgDir := t.TempDir()
	// Entity id says person:alice but file is in wrong directory
	path := writeTestEntity(t, kgDir, "people", "alice.yaml", `id: person:alice
type: Person
attributes:
  name: Alice
`)

	result, err := NormalizeEntityFile(kgDir, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Moved {
		t.Error("expected file to be moved")
	}

	// Old file should be gone
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("expected old file to be removed")
	}

	// New file should exist at correct path
	e, err := ReadEntity(kgDir, "person:alice")
	if err != nil {
		t.Fatalf("reading moved entity: %v", err)
	}
	if e.ID != "person:alice" {
		t.Errorf("expected id person:alice, got %s", e.ID)
	}
}

func TestNormalize_MultipleFixes(t *testing.T) {
	kgDir := t.TempDir()
	path := writeTestEntity(t, kgDir, "person", "alice.yaml", `id: Person:alice
type: PERSON
links:
  knows:
    - Person:bob
`)

	result, err := NormalizeEntityFile(kgDir, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have fixes for: links→relations, id casing, type casing, target casing
	if len(result.Fixes) < 3 {
		t.Errorf("expected at least 3 fixes, got %d: %v", len(result.Fixes), result.Fixes)
	}

	e, err := ReadEntity(kgDir, "person:alice")
	if err != nil {
		t.Fatalf("reading normalized entity: %v", err)
	}
	if e.ID != "person:alice" {
		t.Errorf("expected id person:alice, got %s", e.ID)
	}
	if e.Type != "Person" {
		t.Errorf("expected type Person, got %s", e.Type)
	}
	if len(e.Relations["knows"]) != 1 || e.Relations["knows"][0] != "person:bob" {
		t.Errorf("expected relations.knows=[person:bob], got %v", e.Relations)
	}
}

func TestNormalize_BothLinksAndRelations(t *testing.T) {
	kgDir := t.TempDir()
	path := writeTestEntity(t, kgDir, "person", "alice.yaml", `id: person:alice
type: Person
relations:
  knows:
    - person:bob
links:
  authored:
    - book:foo
`)

	result, err := NormalizeEntityFile(kgDir, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if f := findFix(result, "links"); f == nil {
		t.Error("expected links merge fix")
	}

	e, err := ReadEntity(kgDir, "person:alice")
	if err != nil {
		t.Fatalf("reading normalized entity: %v", err)
	}
	if len(e.Relations["knows"]) != 1 {
		t.Errorf("expected knows relation preserved, got %v", e.Relations["knows"])
	}
	if len(e.Relations["authored"]) != 1 {
		t.Errorf("expected authored relation merged, got %v", e.Relations["authored"])
	}
}

func TestNormalize_PreservesAttributes(t *testing.T) {
	kgDir := t.TempDir()
	path := writeTestEntity(t, kgDir, "person", "alice.yaml", `id: Person:alice
type: Person
attributes:
  name: Alice
  age: 30
  active: true
`)

	result, err := NormalizeEntityFile(kgDir, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Fixes) == 0 {
		t.Fatal("expected at least id fix")
	}

	e, err := ReadEntity(kgDir, "person:alice")
	if err != nil {
		t.Fatalf("reading normalized entity: %v", err)
	}
	if e.Attributes["name"] != "Alice" {
		t.Errorf("expected name=Alice, got %v", e.Attributes["name"])
	}
	if e.Attributes["age"] != 30 {
		t.Errorf("expected age=30, got %v", e.Attributes["age"])
	}
	if e.Attributes["active"] != true {
		t.Errorf("expected active=true, got %v", e.Attributes["active"])
	}
}
