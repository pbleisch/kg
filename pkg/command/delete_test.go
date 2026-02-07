package command

import (
	"testing"

	"github.com/pbleisch/kg/pkg/store"
)

func TestDelete(t *testing.T) {
	kgDir := setupKGDir(t)

	_, err := Create(kgDir, "Person", "charlie", map[string]string{"name": "Charlie"})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if err := Delete(kgDir, "person:charlie"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify entity no longer exists.
	if _, err := store.ReadEntity(kgDir, "person:charlie"); err == nil {
		t.Fatal("expected error reading deleted entity, got nil")
	}
}

func TestDeleteNonexistent(t *testing.T) {
	kgDir := setupKGDir(t)

	err := Delete(kgDir, "person:nobody")
	if err == nil {
		t.Fatal("expected error deleting nonexistent entity, got nil")
	}
}
