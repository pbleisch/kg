package command

import (
	"testing"

	"github.com/pbleisch/kg/pkg/store"
)

func TestSetExistingAttrs(t *testing.T) {
	kgDir := setupKGDir(t)

	_, err := Create(kgDir, "Person", "alice", map[string]string{
		"name": "Alice",
		"role": "Engineer",
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	updated, err := Set(kgDir, "person:alice", map[string]string{
		"role": "Senior Engineer",
	})
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	if updated.Attributes["role"] != "Senior Engineer" {
		t.Errorf("role = %v, want %q", updated.Attributes["role"], "Senior Engineer")
	}
	if updated.Attributes["name"] != "Alice" {
		t.Errorf("name = %v, want %q (should be preserved)", updated.Attributes["name"], "Alice")
	}

	// Verify persistence.
	got, err := store.ReadEntity(kgDir, "person:alice")
	if err != nil {
		t.Fatalf("ReadEntity failed: %v", err)
	}
	if got.Attributes["role"] != "Senior Engineer" {
		t.Errorf("persisted role = %v, want %q", got.Attributes["role"], "Senior Engineer")
	}
}

func TestSetNewAttrs(t *testing.T) {
	kgDir := setupKGDir(t)

	_, err := Create(kgDir, "Person", "bob", map[string]string{
		"name": "Bob",
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	updated, err := Set(kgDir, "person:bob", map[string]string{
		"email": "bob@example.com",
		"role":  "Designer",
	})
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	if updated.Attributes["email"] != "bob@example.com" {
		t.Errorf("email = %v, want %q", updated.Attributes["email"], "bob@example.com")
	}
	if updated.Attributes["role"] != "Designer" {
		t.Errorf("role = %v, want %q", updated.Attributes["role"], "Designer")
	}
	if updated.Attributes["name"] != "Bob" {
		t.Errorf("name = %v, want %q (should be preserved)", updated.Attributes["name"], "Bob")
	}
}

func TestSetNonexistentEntity(t *testing.T) {
	kgDir := setupKGDir(t)

	_, err := Set(kgDir, "person:nobody", map[string]string{"role": "Ghost"})
	if err == nil {
		t.Fatal("expected error for nonexistent entity, got nil")
	}
}
