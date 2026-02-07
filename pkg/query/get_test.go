package query

import (
	"errors"
	"testing"

	"github.com/pbleisch/kg/pkg/store"
)

func TestGetExistingEntity(t *testing.T) {
	dir := t.TempDir()

	e := &store.Entity{
		ID:         "person:alice",
		Type:       "Person",
		Attributes: map[string]any{"name": "Alice"},
	}
	if err := store.WriteEntity(dir, e); err != nil {
		t.Fatalf("WriteEntity: %v", err)
	}

	got, err := Get(dir, "person:alice")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != "person:alice" {
		t.Errorf("ID = %q, want person:alice", got.ID)
	}
	if got.Type != "Person" {
		t.Errorf("Type = %q, want Person", got.Type)
	}
	if got.Attributes["name"] != "Alice" {
		t.Errorf("name = %v, want Alice", got.Attributes["name"])
	}
}

func TestGetNonExistentEntity(t *testing.T) {
	dir := t.TempDir()

	_, err := Get(dir, "person:nobody")
	if err == nil {
		t.Fatal("expected error for non-existent entity")
	}

	var qe *QueryError
	if !errors.As(err, &qe) {
		t.Fatalf("expected QueryError, got %T: %v", err, err)
	}
	if qe.Code != "entity_not_found" {
		t.Errorf("Code = %q, want entity_not_found", qe.Code)
	}
}
