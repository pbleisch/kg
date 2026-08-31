package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseEntityID(t *testing.T) {
	tests := []struct {
		id        string
		wantType  string
		wantLocal string
		wantErr   bool
	}{
		{"person:alice", "person", "alice", false},
		{"project:atlas", "project", "atlas", false},
		{"org:acme:labs", "org", "acme:labs", false},
		{"", "", "", true},
		{"nocolon", "", "", true},
		{":noprefix", "", "", true},
		{"nosuffix:", "", "", true},
	}

	for _, tt := range tests {
		typeName, localID, err := ParseEntityID(tt.id)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseEntityID(%q) error = %v, wantErr %v", tt.id, err, tt.wantErr)
			continue
		}
		if typeName != tt.wantType || localID != tt.wantLocal {
			t.Errorf("ParseEntityID(%q) = (%q, %q), want (%q, %q)", tt.id, typeName, localID, tt.wantType, tt.wantLocal)
		}
	}
}

func TestEntityPath(t *testing.T) {
	got := EntityPath("/tmp/kg", "person:alice")
	want := filepath.Join("/tmp/kg", "entities", "person", "alice.yaml")
	if got != want {
		t.Errorf("EntityPath = %q, want %q", got, want)
	}
}

func TestEntityPathEncoding(t *testing.T) {
	got := EntityPath("/tmp/kg", "person:alice chen")
	want := filepath.Join("/tmp/kg", "entities", "person", "alice%20chen.yaml")
	if got != want {
		t.Errorf("EntityPath encoding = %q, want %q", got, want)
	}
}

func TestEntityPathInvalid(t *testing.T) {
	got := EntityPath("/tmp/kg", "invalid")
	if got != "" {
		t.Errorf("EntityPath invalid = %q, want empty", got)
	}
}

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()

	entity := &Entity{
		ID:   "person:alice",
		Type: "Person",
		Attributes: map[string]any{
			"name": "Alice Chen",
			"role": "Engineer",
		},
		Relations: map[string][]string{
			"worksOn":   {"project:atlas"},
			"reportsTo": {"person:bob"},
		},
	}

	if err := WriteEntity(dir, entity); err != nil {
		t.Fatalf("WriteEntity: %v", err)
	}

	// Verify file exists at expected path.
	path := EntityPath(dir, "person:alice")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file at %s: %v", path, err)
	}

	got, err := ReadEntity(dir, "person:alice")
	if err != nil {
		t.Fatalf("ReadEntity: %v", err)
	}

	if got.ID != entity.ID {
		t.Errorf("ID = %q, want %q", got.ID, entity.ID)
	}
	if got.Type != entity.Type {
		t.Errorf("Type = %q, want %q", got.Type, entity.Type)
	}
	if got.Attributes["name"] != "Alice Chen" {
		t.Errorf("Attributes[name] = %v, want Alice Chen", got.Attributes["name"])
	}
	rels := got.Relations["worksOn"]
	if len(rels) != 1 || rels[0] != "project:atlas" {
		t.Errorf("Relations[worksOn] = %v, want [project:atlas]", rels)
	}
}

func TestDeleteEntity(t *testing.T) {
	dir := t.TempDir()

	entity := &Entity{
		ID:   "person:bob",
		Type: "Person",
		Attributes: map[string]any{
			"name": "Bob",
		},
	}

	if err := WriteEntity(dir, entity); err != nil {
		t.Fatalf("WriteEntity: %v", err)
	}

	if err := DeleteEntity(dir, "person:bob"); err != nil {
		t.Fatalf("DeleteEntity: %v", err)
	}

	path := EntityPath(dir, "person:bob")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected file to be deleted, got err: %v", err)
	}
}

func TestListEntities(t *testing.T) {
	dir := t.TempDir()

	entities := []*Entity{
		{ID: "person:alice", Type: "Person", Attributes: map[string]any{"name": "Alice"}},
		{ID: "person:bob", Type: "Person", Attributes: map[string]any{"name": "Bob"}},
		{ID: "project:atlas", Type: "Project", Attributes: map[string]any{"name": "Atlas"}},
	}

	for _, e := range entities {
		if err := WriteEntity(dir, e); err != nil {
			t.Fatalf("WriteEntity(%s): %v", e.ID, err)
		}
	}

	all, err := ListEntities(dir)
	if err != nil {
		t.Fatalf("ListEntities: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("ListEntities count = %d, want 3", len(all))
	}

	people, err := ListEntitiesByType(dir, "person")
	if err != nil {
		t.Fatalf("ListEntitiesByType: %v", err)
	}
	if len(people) != 2 {
		t.Errorf("ListEntitiesByType(person) count = %d, want 2", len(people))
	}

	projects, err := ListEntitiesByType(dir, "project")
	if err != nil {
		t.Fatalf("ListEntitiesByType: %v", err)
	}
	if len(projects) != 1 {
		t.Errorf("ListEntitiesByType(project) count = %d, want 1", len(projects))
	}
}
