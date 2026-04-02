package command

import (
	"testing"

	"github.com/pbleisch/kg/pkg/store"
)

// makeEntityWithEmbedding creates an entity with a vector embedding attribute.
func makeEntityWithEmbedding(id, typeName, name string, vec []float64) *store.Entity {
	return &store.Entity{
		ID:   id,
		Type: typeName,
		Attributes: map[string]any{
			"name":           name,
			"name_embedding": vec,
		},
	}
}

func setupDedupDir(t *testing.T, entities []*store.Entity) string {
	t.Helper()
	dir := t.TempDir()
	for _, e := range entities {
		if err := store.WriteEntity(dir, e); err != nil {
			t.Fatalf("WriteEntity(%s): %v", e.ID, err)
		}
	}
	if err := store.Sync(dir); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	return dir
}

func TestFindDuplicates_NoDuplicates(t *testing.T) {
	entities := []*store.Entity{
		makeEntityWithEmbedding("concept:alpha", "concept", "Alpha", []float64{1, 0, 0}),
		makeEntityWithEmbedding("concept:beta", "concept", "Beta", []float64{0, 1, 0}),
		makeEntityWithEmbedding("concept:gamma", "concept", "Gamma", []float64{0, 0, 1}),
	}
	dir := setupDedupDir(t, entities)

	result, err := FindDuplicates(dir, "concept", DedupOptions{
		Threshold: 0.85,
		Fields:    []string{"name"},
	})
	if err != nil {
		t.Fatalf("FindDuplicates: %v", err)
	}
	if result.ClustersFound != 0 {
		t.Errorf("expected 0 clusters, got %d", result.ClustersFound)
	}
	if result.TotalEntities != 3 {
		t.Errorf("expected 3 total entities, got %d", result.TotalEntities)
	}
}

func TestFindDuplicates_IdenticalVectors(t *testing.T) {
	vec := []float64{0.5, 0.5, 0.5}
	entities := []*store.Entity{
		makeEntityWithEmbedding("concept:a", "concept", "Concept A", vec),
		makeEntityWithEmbedding("concept:b", "concept", "Concept B", vec),
		makeEntityWithEmbedding("concept:c", "concept", "Other", []float64{0, 0, 1}),
	}
	dir := setupDedupDir(t, entities)

	result, err := FindDuplicates(dir, "concept", DedupOptions{
		Threshold: 0.99,
		Fields:    []string{"name"},
	})
	if err != nil {
		t.Fatalf("FindDuplicates: %v", err)
	}
	if result.ClustersFound != 1 {
		t.Errorf("expected 1 cluster, got %d", result.ClustersFound)
	}
	if result.DuplicatesFound != 1 {
		t.Errorf("expected 1 duplicate, got %d", result.DuplicatesFound)
	}
	if result.Clusters[0].CanonicalID != "concept:a" {
		t.Errorf("expected canonical concept:a, got %s", result.Clusters[0].CanonicalID)
	}
	if result.Clusters[0].Duplicates[0].ID != "concept:b" {
		t.Errorf("expected duplicate concept:b, got %s", result.Clusters[0].Duplicates[0].ID)
	}
}

func TestFindDuplicates_TransitiveClustering(t *testing.T) {
	// A≈B (high similarity), B≈C (high similarity), A and C less similar
	// but should still be in the same cluster via transitivity.
	entities := []*store.Entity{
		makeEntityWithEmbedding("concept:a", "concept", "A", []float64{1, 0.1, 0}),
		makeEntityWithEmbedding("concept:b", "concept", "B", []float64{0.9, 0.2, 0.1}),
		makeEntityWithEmbedding("concept:c", "concept", "C", []float64{0.8, 0.3, 0.2}),
		makeEntityWithEmbedding("concept:z", "concept", "Z", []float64{0, 0, 1}),
	}
	dir := setupDedupDir(t, entities)

	result, err := FindDuplicates(dir, "concept", DedupOptions{
		Threshold: 0.90,
		Fields:    []string{"name"},
	})
	if err != nil {
		t.Fatalf("FindDuplicates: %v", err)
	}
	if result.ClustersFound != 1 {
		t.Errorf("expected 1 cluster (transitive), got %d", result.ClustersFound)
	}
	if result.ClustersFound > 0 && result.Clusters[0].CanonicalID != "concept:a" {
		t.Errorf("expected canonical concept:a, got %s", result.Clusters[0].CanonicalID)
	}
}

func TestFindDuplicates_ThresholdFiltering(t *testing.T) {
	// Two fairly similar vectors but not identical.
	entities := []*store.Entity{
		makeEntityWithEmbedding("concept:x", "concept", "X", []float64{1, 0, 0}),
		makeEntityWithEmbedding("concept:y", "concept", "Y", []float64{0.9, 0.44, 0}),
	}
	dir := setupDedupDir(t, entities)

	// High threshold: no match.
	r1, _ := FindDuplicates(dir, "concept", DedupOptions{
		Threshold: 0.99,
		Fields:    []string{"name"},
	})
	if r1.ClustersFound != 0 {
		t.Errorf("high threshold: expected 0 clusters, got %d", r1.ClustersFound)
	}

	// Lower threshold: match.
	r2, _ := FindDuplicates(dir, "concept", DedupOptions{
		Threshold: 0.85,
		Fields:    []string{"name"},
	})
	if r2.ClustersFound != 1 {
		t.Errorf("low threshold: expected 1 cluster, got %d", r2.ClustersFound)
	}
}

func TestFindDuplicates_MissingEmbeddings(t *testing.T) {
	entities := []*store.Entity{
		{ID: "concept:no_vec", Type: "concept", Attributes: map[string]any{"name": "No Vector"}},
		makeEntityWithEmbedding("concept:has_vec", "concept", "Has Vector", []float64{1, 0, 0}),
	}
	dir := setupDedupDir(t, entities)

	result, err := FindDuplicates(dir, "concept", DedupOptions{
		Threshold: 0.85,
		Fields:    []string{"name"},
	})
	if err != nil {
		t.Fatalf("FindDuplicates: %v", err)
	}
	if len(result.Warnings) == 0 {
		t.Error("expected warning for entity without embeddings")
	}
	if result.ClustersFound != 0 {
		t.Errorf("expected 0 clusters, got %d", result.ClustersFound)
	}
}

func TestFindDuplicates_EmptyType(t *testing.T) {
	dir := t.TempDir()
	result, err := FindDuplicates(dir, "nonexistent", DedupOptions{
		Threshold: 0.85,
		Fields:    []string{"name"},
	})
	if err != nil {
		t.Fatalf("FindDuplicates: %v", err)
	}
	if result.TotalEntities != 0 {
		t.Errorf("expected 0 entities, got %d", result.TotalEntities)
	}
}

func TestApplyDedup(t *testing.T) {
	vec := []float64{0.5, 0.5, 0.5}
	entities := []*store.Entity{
		makeEntityWithEmbedding("concept:canonical", "concept", "Canonical", vec),
		makeEntityWithEmbedding("concept:duplicate", "concept", "Duplicate", vec),
		makeEntityWithEmbedding("concept:unique", "concept", "Unique", []float64{0, 0, 1}),
	}
	dir := setupDedupDir(t, entities)

	result, _ := FindDuplicates(dir, "concept", DedupOptions{
		Threshold: 0.99,
		Fields:    []string{"name"},
	})

	if result.ClustersFound == 0 {
		t.Fatal("expected clusters to apply")
	}

	opts := DedupOptions{Threshold: 0.99, Fields: []string{"name"}}
	if err := ApplyDedup(dir, result, opts); err != nil {
		t.Fatalf("ApplyDedup: %v", err)
	}
	if result.Merged != 1 {
		t.Errorf("expected 1 merged, got %d", result.Merged)
	}

	// Duplicate should be gone.
	if _, err := store.ReadEntity(dir, "concept:duplicate"); err == nil {
		t.Error("concept:duplicate should have been deleted after merge")
	}
	// Canonical should still exist.
	if _, err := store.ReadEntity(dir, "concept:canonical"); err != nil {
		t.Error("concept:canonical should still exist after merge")
	}
	// Unique should be untouched.
	if _, err := store.ReadEntity(dir, "concept:unique"); err != nil {
		t.Error("concept:unique should still exist")
	}
}

func TestApplyDedup_DryRunDoesNotModify(t *testing.T) {
	vec := []float64{0.5, 0.5, 0.5}
	entities := []*store.Entity{
		makeEntityWithEmbedding("concept:a", "concept", "A", vec),
		makeEntityWithEmbedding("concept:b", "concept", "B", vec),
	}
	dir := setupDedupDir(t, entities)

	result, _ := FindDuplicates(dir, "concept", DedupOptions{
		Threshold: 0.99,
		Fields:    []string{"name"},
		DryRun:    true,
	})
	if result.ClustersFound == 0 {
		t.Fatal("expected clusters")
	}

	// Don't call ApplyDedup — dry run.
	// Both entities should still exist.
	if _, err := store.ReadEntity(dir, "concept:a"); err != nil {
		t.Error("concept:a should still exist in dry run")
	}
	if _, err := store.ReadEntity(dir, "concept:b"); err != nil {
		t.Error("concept:b should still exist in dry run")
	}
}

func TestFindDuplicates_CanonicalIsLowestID(t *testing.T) {
	vec := []float64{1, 0, 0}
	entities := []*store.Entity{
		makeEntityWithEmbedding("concept:zebra", "concept", "Zebra", vec),
		makeEntityWithEmbedding("concept:apple", "concept", "Apple", vec),
	}
	dir := setupDedupDir(t, entities)

	result, _ := FindDuplicates(dir, "concept", DedupOptions{
		Threshold: 0.99,
		Fields:    []string{"name"},
	})
	if result.ClustersFound != 1 {
		t.Fatalf("expected 1 cluster, got %d", result.ClustersFound)
	}
	if result.Clusters[0].CanonicalID != "concept:apple" {
		t.Errorf("expected canonical concept:apple (lowest), got %s", result.Clusters[0].CanonicalID)
	}
}
