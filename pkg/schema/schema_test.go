package schema

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pbleisch/kg/pkg/store"
)

// setupSchema writes a schema YAML file under a temporary .kg/schema/ dir.
func setupSchema(t *testing.T, kgDir, filename, content string) {
	t.Helper()
	dir := filepath.Join(kgDir, "schema")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const personSchema = `type: Person
description: A person in the organization
attributes:
  name:
    type: string
    required: true
  role:
    type: string
  email:
    type: string
relations:
  worksOn:
    target: Project
    cardinality: many
  reportsTo:
    target: Person
    cardinality: one
`

const projectSchema = `type: Project
description: A project
attributes:
  title:
    type: string
    required: true
  budget:
    type: number
relations: {}
`

func TestLoadSchema(t *testing.T) {
	kgDir := t.TempDir()
	setupSchema(t, kgDir, "person.yaml", personSchema)

	s, err := LoadSchema(kgDir, "person")
	if err != nil {
		t.Fatalf("LoadSchema: %v", err)
	}
	if s.Type != "Person" {
		t.Errorf("Type = %q, want Person", s.Type)
	}
	if s.Description != "A person in the organization" {
		t.Errorf("Description = %q", s.Description)
	}
	if len(s.Attributes) != 3 {
		t.Errorf("got %d attributes, want 3", len(s.Attributes))
	}
	if !s.Attributes["name"].Required {
		t.Error("name should be required")
	}
	if s.Relations["worksOn"].Cardinality != "many" {
		t.Error("worksOn cardinality should be many")
	}
	if s.Relations["reportsTo"].Target != "Person" {
		t.Error("reportsTo target should be Person")
	}
}

func TestLoadAllSchemas(t *testing.T) {
	kgDir := t.TempDir()
	setupSchema(t, kgDir, "person.yaml", personSchema)
	setupSchema(t, kgDir, "project.yaml", projectSchema)

	schemas, err := LoadAllSchemas(kgDir)
	if err != nil {
		t.Fatalf("LoadAllSchemas: %v", err)
	}
	if len(schemas) != 2 {
		t.Fatalf("got %d schemas, want 2", len(schemas))
	}
	if schemas["Person"] == nil {
		t.Error("missing Person schema")
	}
	if schemas["Project"] == nil {
		t.Error("missing Project schema")
	}
}

func TestValidateEntity_Valid(t *testing.T) {
	kgDir := t.TempDir()
	setupSchema(t, kgDir, "person.yaml", personSchema)
	s, _ := LoadSchema(kgDir, "person")

	entity := &store.Entity{
		ID:   "person:alice",
		Type: "Person",
		Attributes: map[string]any{
			"name":  "Alice",
			"role":  "Engineer",
			"email": "alice@example.com",
		},
		Relations: map[string][]string{
			"worksOn":   {"project:kg"},
			"reportsTo": {"person:bob"},
		},
	}

	errs := ValidateEntity(s, entity)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestValidateEntity_MissingRequired(t *testing.T) {
	kgDir := t.TempDir()
	setupSchema(t, kgDir, "person.yaml", personSchema)
	s, _ := LoadSchema(kgDir, "person")

	entity := &store.Entity{
		ID:         "person:alice",
		Type:       "Person",
		Attributes: map[string]any{
			"role": "Engineer",
		},
	}

	errs := ValidateEntity(s, entity)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	if errs[0].Field != "attributes.name" {
		t.Errorf("field = %q, want attributes.name", errs[0].Field)
	}
}

func TestValidateEntity_WrongType(t *testing.T) {
	kgDir := t.TempDir()
	setupSchema(t, kgDir, "project.yaml", projectSchema)
	s, _ := LoadSchema(kgDir, "project")

	entity := &store.Entity{
		ID:   "project:kg",
		Type: "Project",
		Attributes: map[string]any{
			"title":  "KG Project",
			"budget": "not-a-number",
		},
	}

	errs := ValidateEntity(s, entity)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	if errs[0].Field != "attributes.budget" {
		t.Errorf("field = %q, want attributes.budget", errs[0].Field)
	}
}

func TestValidateEntity_CardinalityViolation(t *testing.T) {
	kgDir := t.TempDir()
	setupSchema(t, kgDir, "person.yaml", personSchema)
	s, _ := LoadSchema(kgDir, "person")

	entity := &store.Entity{
		ID:   "person:alice",
		Type: "Person",
		Attributes: map[string]any{
			"name": "Alice",
		},
		Relations: map[string][]string{
			"reportsTo": {"person:bob", "person:carol"},
		},
	}

	errs := ValidateEntity(s, entity)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	if errs[0].Field != "relations.reportsTo" {
		t.Errorf("field = %q, want relations.reportsTo", errs[0].Field)
	}
}
