package query

import (
	"slices"
	"testing"

	"github.com/pbleisch/kg/pkg/store"
)

// setupTestKG creates a temp kg directory with entities and syncs the index.
func setupTestKG(t *testing.T, entities []*store.Entity) string {
	t.Helper()
	kgDir := t.TempDir()

	for _, e := range entities {
		if err := store.WriteEntity(kgDir, e); err != nil {
			t.Fatalf("WriteEntity(%s): %v", e.ID, err)
		}
	}
	if err := store.Sync(kgDir); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	return kgDir
}

func TestBack_FindsEntitiesPointingToTarget(t *testing.T) {
	kgDir := setupTestKG(t, []*store.Entity{
		{
			ID:   "person:alice",
			Type: "person",
			Attributes: map[string]any{"name": "Alice"},
			Relations:  map[string][]string{"worksOn": {"project:atlas"}},
		},
		{
			ID:   "person:bob",
			Type: "person",
			Attributes: map[string]any{"name": "Bob"},
			Relations:  map[string][]string{"worksOn": {"project:atlas"}},
		},
		{
			ID:         "project:atlas",
			Type:       "project",
			Attributes: map[string]any{"name": "Atlas"},
		},
	})

	entities, err := Back(kgDir, "project:atlas", "worksOn")
	if err != nil {
		t.Fatalf("Back: %v", err)
	}

	var ids []string
	for _, e := range entities {
		ids = append(ids, e.ID)
	}
	slices.Sort(ids)

	if len(ids) != 2 || ids[0] != "person:alice" || ids[1] != "person:bob" {
		t.Errorf("Back() = %v, want [person:alice person:bob]", ids)
	}
}

func TestBack_ReturnsEmptyForNoMatches(t *testing.T) {
	kgDir := setupTestKG(t, []*store.Entity{
		{
			ID:         "project:atlas",
			Type:       "project",
			Attributes: map[string]any{"name": "Atlas"},
		},
	})

	entities, err := Back(kgDir, "project:atlas", "worksOn")
	if err != nil {
		t.Fatalf("Back: %v", err)
	}

	if len(entities) != 0 {
		t.Errorf("Back() = %v, want empty", entities)
	}
}

func TestBackRecursive_FollowsChains(t *testing.T) {
	// Chain: C dependsOn B dependsOn A
	kgDir := setupTestKG(t, []*store.Entity{
		{
			ID:         "pkg:a",
			Type:       "package",
			Attributes: map[string]any{"name": "A"},
		},
		{
			ID:         "pkg:b",
			Type:       "package",
			Attributes: map[string]any{"name": "B"},
			Relations:  map[string][]string{"dependsOn": {"pkg:a"}},
		},
		{
			ID:         "pkg:c",
			Type:       "package",
			Attributes: map[string]any{"name": "C"},
			Relations:  map[string][]string{"dependsOn": {"pkg:b"}},
		},
	})

	ids, err := BackRecursive(kgDir, "pkg:a", "dependsOn")
	if err != nil {
		t.Fatalf("BackRecursive: %v", err)
	}
	slices.Sort(ids)

	if len(ids) != 2 || ids[0] != "pkg:b" || ids[1] != "pkg:c" {
		t.Errorf("BackRecursive() = %v, want [pkg:b pkg:c]", ids)
	}
}

func TestBackRecursive_HandlesCycles(t *testing.T) {
	// A -> B -> A (cycle)
	kgDir := setupTestKG(t, []*store.Entity{
		{
			ID:        "pkg:a",
			Type:      "package",
			Relations: map[string][]string{"dependsOn": {"pkg:b"}},
		},
		{
			ID:        "pkg:b",
			Type:      "package",
			Relations: map[string][]string{"dependsOn": {"pkg:a"}},
		},
	})

	ids, err := BackRecursive(kgDir, "pkg:a", "dependsOn")
	if err != nil {
		t.Fatalf("BackRecursive: %v", err)
	}

	// Should find pkg:b (which depends on pkg:a), and then pkg:a depends on pkg:b
	// but pkg:a is already seen (it's the starting node), so just pkg:b.
	if len(ids) != 1 || ids[0] != "pkg:b" {
		t.Errorf("BackRecursive() = %v, want [pkg:b]", ids)
	}
}
