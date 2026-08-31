package command

import (
	"testing"

	"github.com/pbleisch/kg/pkg/store"
)

func TestLinkAddsRelation(t *testing.T) {
	kgDir := setupKGDir(t)
	createTestEntity(t, kgDir, "person:charlie")

	if err := Link(kgDir, "person:charlie", "worksOn", "project:atlas"); err != nil {
		t.Fatalf("Link failed: %v", err)
	}

	e, err := store.ReadEntity(kgDir, "person:charlie")
	if err != nil {
		t.Fatalf("ReadEntity failed: %v", err)
	}
	targets := e.Relations["worksOn"]
	if len(targets) != 1 || targets[0] != "project:atlas" {
		t.Errorf("Relations[worksOn] = %v, want [project:atlas]", targets)
	}
}

func TestLinkAvoidsDuplicates(t *testing.T) {
	kgDir := setupKGDir(t)
	createTestEntity(t, kgDir, "person:charlie")

	if err := Link(kgDir, "person:charlie", "worksOn", "project:atlas"); err != nil {
		t.Fatalf("first Link failed: %v", err)
	}
	if err := Link(kgDir, "person:charlie", "worksOn", "project:atlas"); err != nil {
		t.Fatalf("second Link failed: %v", err)
	}

	e, err := store.ReadEntity(kgDir, "person:charlie")
	if err != nil {
		t.Fatalf("ReadEntity failed: %v", err)
	}
	if len(e.Relations["worksOn"]) != 1 {
		t.Errorf("expected 1 target, got %d", len(e.Relations["worksOn"]))
	}
}

func TestLinkRefusesMissingSource(t *testing.T) {
	kgDir := setupKGDir(t)

	// A missing source used to become a typeless stub entity.  It is now an
	// error: the store does not acquire entities nobody submitted.
	if err := Link(kgDir, "person:ghost", "worksOn", "project:atlas"); err == nil {
		t.Fatal("expected an error linking from a nonexistent source")
	}
	if _, err := store.ReadEntity(kgDir, "person:ghost"); err == nil {
		t.Fatal("a stub entity was created for the missing source")
	}
}

func TestUnlinkRemovesRelation(t *testing.T) {
	kgDir := setupKGDir(t)
	createTestEntity(t, kgDir, "person:charlie")

	if err := Link(kgDir, "person:charlie", "worksOn", "project:atlas"); err != nil {
		t.Fatalf("Link failed: %v", err)
	}
	if err := Unlink(kgDir, "person:charlie", "worksOn", "project:atlas"); err != nil {
		t.Fatalf("Unlink failed: %v", err)
	}

	e, err := store.ReadEntity(kgDir, "person:charlie")
	if err != nil {
		t.Fatalf("ReadEntity failed: %v", err)
	}
	if len(e.Relations) != 0 {
		t.Errorf("expected empty relations, got %v", e.Relations)
	}
}

func TestUnlinkNonexistentRelation(t *testing.T) {
	kgDir := setupKGDir(t)
	createTestEntity(t, kgDir, "person:charlie")

	err := Unlink(kgDir, "person:charlie", "worksOn", "project:atlas")
	if err == nil {
		t.Fatal("expected error for nonexistent relation, got nil")
	}
}

func TestUnlinkNonexistentTarget(t *testing.T) {
	kgDir := setupKGDir(t)
	createTestEntity(t, kgDir, "person:charlie")

	if err := Link(kgDir, "person:charlie", "worksOn", "project:atlas"); err != nil {
		t.Fatalf("Link failed: %v", err)
	}

	err := Unlink(kgDir, "person:charlie", "worksOn", "project:other")
	if err == nil {
		t.Fatal("expected error for nonexistent target, got nil")
	}
}

// createTestEntity writes a minimal entity for testing.
func createTestEntity(t *testing.T, kgDir, id string) {
	t.Helper()
	typeName, _, err := store.ParseEntityID(id)
	if err != nil {
		t.Fatal(err)
	}
	e := &store.Entity{
		ID:         id,
		Type:       typeName,
		Attributes: map[string]any{"name": "Test"},
	}
	if err := store.WriteEntity(kgDir, e); err != nil {
		t.Fatalf("WriteEntity failed: %v", err)
	}
}
