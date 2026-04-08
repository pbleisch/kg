package store

import (
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func tempIndex(t *testing.T) Indexer {
	t.Helper()
	dir := t.TempDir()
	idx, err := OpenIndex(dir)
	if err != nil {
		t.Fatalf("OpenIndex: %v", err)
	}
	t.Cleanup(func() { idx.Close() })
	return idx
}

func TestOpenClose(t *testing.T) {
	dir := t.TempDir()
	idx, err := OpenIndex(dir)
	if err != nil {
		t.Fatalf("OpenIndex: %v", err)
	}
	if err := idx.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Re-open the same store.
	idx2, err := OpenIndex(dir)
	if err != nil {
		t.Fatalf("OpenIndex (reopen): %v", err)
	}
	if err := idx2.Close(); err != nil {
		t.Fatalf("Close (reopen): %v", err)
	}
}

func TestAddGetEntity(t *testing.T) {
	idx := tempIndex(t)

	e := &Entity{
		ID:   "alice",
		Type: "person",
		Attributes: map[string]any{
			"name": "Alice",
			"age":  "30",
		},
		Relations: map[string][]string{
			"knows": {"bob", "carol"},
		},
	}

	if err := idx.AddEntity(e); err != nil {
		t.Fatalf("AddEntity: %v", err)
	}

	got, err := idx.GetEntity("alice")
	if err != nil {
		t.Fatalf("GetEntity: %v", err)
	}

	if got.ID != "alice" {
		t.Errorf("ID = %q, want %q", got.ID, "alice")
	}
	if got.Type != "person" {
		t.Errorf("Type = %q, want %q", got.Type, "person")
	}
	if got.Attributes["name"] != "Alice" {
		t.Errorf("Attributes[name] = %v, want %q", got.Attributes["name"], "Alice")
	}
	if got.Attributes["age"] != "30" {
		t.Errorf("Attributes[age] = %v, want %q", got.Attributes["age"], "30")
	}

	knows := got.Relations["knows"]
	slices.Sort(knows)
	if len(knows) != 2 || knows[0] != "bob" || knows[1] != "carol" {
		t.Errorf("Relations[knows] = %v, want [bob carol]", knows)
	}
}

func TestGetEntityNotFound(t *testing.T) {
	idx := tempIndex(t)

	_, err := idx.GetEntity("nonexistent")
	if err == nil {
		t.Fatal("expected error for missing entity")
	}
}

func TestRemoveEntity(t *testing.T) {
	idx := tempIndex(t)

	e := &Entity{
		ID:         "bob",
		Type:       "person",
		Attributes: map[string]any{"name": "Bob"},
	}

	if err := idx.AddEntity(e); err != nil {
		t.Fatalf("AddEntity: %v", err)
	}

	// Verify it exists.
	if _, err := idx.GetEntity("bob"); err != nil {
		t.Fatalf("GetEntity before remove: %v", err)
	}

	if err := idx.RemoveEntity("bob"); err != nil {
		t.Fatalf("RemoveEntity: %v", err)
	}

	// Should be gone now.
	_, err := idx.GetEntity("bob")
	if err == nil {
		t.Fatal("expected error after RemoveEntity")
	}

	// Removing again should be a no-op.
	if err := idx.RemoveEntity("bob"); err != nil {
		t.Fatalf("RemoveEntity (second): %v", err)
	}
}

func TestFindByType(t *testing.T) {
	idx := tempIndex(t)

	entities := []*Entity{
		{ID: "alice", Type: "person", Attributes: map[string]any{}},
		{ID: "bob", Type: "person", Attributes: map[string]any{}},
		{ID: "acme", Type: "company", Attributes: map[string]any{}},
	}
	for _, e := range entities {
		if err := idx.AddEntity(e); err != nil {
			t.Fatalf("AddEntity(%s): %v", e.ID, err)
		}
	}

	people, err := idx.FindByType("person")
	if err != nil {
		t.Fatalf("FindByType: %v", err)
	}
	slices.Sort(people)
	if len(people) != 2 || people[0] != "alice" || people[1] != "bob" {
		t.Errorf("FindByType(person) = %v, want [alice bob]", people)
	}

	companies, err := idx.FindByType("company")
	if err != nil {
		t.Fatalf("FindByType: %v", err)
	}
	if len(companies) != 1 || companies[0] != "acme" {
		t.Errorf("FindByType(company) = %v, want [acme]", companies)
	}

	// Non-existent type.
	empty, err := idx.FindByType("robot")
	if err != nil {
		t.Fatalf("FindByType: %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("FindByType(robot) = %v, want []", empty)
	}
}

func TestCacheDirCreated(t *testing.T) {
	dir := t.TempDir()
	idx, err := OpenIndex(dir)
	if err != nil {
		t.Fatalf("OpenIndex: %v", err)
	}
	defer idx.Close()

	if ladybugAvailable {
		// With ladybug, the database is a directory created by LadybugDB.
		dbPath := filepath.Join(dir, ".cache", "ladybug")
		if _, err := os.Stat(dbPath); err != nil {
			t.Fatalf("ladybug db dir not created: %v", err)
		}
	} else {
		dbPath := dir + "/.cache/cayley.bolt"
		info, err := os.Stat(dbPath)
		if err != nil {
			t.Fatalf("db file not created: %v", err)
		}
		if info.IsDir() {
			t.Fatal("db path should be a file, not a directory")
		}
	}
}

func TestFindReferrers(t *testing.T) {
	idx := tempIndex(t)

	entities := []*Entity{
		{ID: "alice", Type: "person", Relations: map[string][]string{"knows": {"bob"}, "works_with": {"bob"}}},
		{ID: "carol", Type: "person", Relations: map[string][]string{"knows": {"bob"}}},
		{ID: "dave", Type: "person", Relations: map[string][]string{"knows": {"carol"}}},
		{ID: "bob", Type: "person"},
	}
	for _, e := range entities {
		if err := idx.AddEntity(e); err != nil {
			t.Fatalf("AddEntity(%s): %v", e.ID, err)
		}
	}

	// Bob is referenced by alice (knows + works_with) and carol (knows).
	refs, err := idx.FindReferrers("bob")
	if err != nil {
		t.Fatalf("FindReferrers(bob): %v", err)
	}
	slices.Sort(refs)
	if len(refs) != 2 || refs[0] != "alice" || refs[1] != "carol" {
		t.Errorf("FindReferrers(bob) = %v, want [alice carol]", refs)
	}

	// Carol is referenced only by dave.
	refs, err = idx.FindReferrers("carol")
	if err != nil {
		t.Fatalf("FindReferrers(carol): %v", err)
	}
	if len(refs) != 1 || refs[0] != "dave" {
		t.Errorf("FindReferrers(carol) = %v, want [dave]", refs)
	}

	// Nobody references alice.
	refs, err = idx.FindReferrers("alice")
	if err != nil {
		t.Fatalf("FindReferrers(alice): %v", err)
	}
	if len(refs) != 0 {
		t.Errorf("FindReferrers(alice) = %v, want []", refs)
	}
}

func TestUpsertEntity(t *testing.T) {
	idx := tempIndex(t)

	e := &Entity{
		ID:         "alice",
		Type:       "person",
		Attributes: map[string]any{"name": "Alice", "age": "30"},
		Relations:  map[string][]string{"knows": {"bob"}},
	}
	if err := idx.AddEntity(e); err != nil {
		t.Fatalf("AddEntity: %v", err)
	}

	// Upsert with changed data.
	e2 := &Entity{
		ID:         "alice",
		Type:       "person",
		Attributes: map[string]any{"name": "Alice Smith", "email": "alice@example.com"},
		Relations:  map[string][]string{"knows": {"carol"}},
	}
	if err := idx.UpsertEntity(e2); err != nil {
		t.Fatalf("UpsertEntity: %v", err)
	}

	got, err := idx.GetEntity("alice")
	if err != nil {
		t.Fatalf("GetEntity: %v", err)
	}
	if got.Attributes["name"] != "Alice Smith" {
		t.Errorf("name = %v, want Alice Smith", got.Attributes["name"])
	}
	if got.Attributes["email"] != "alice@example.com" {
		t.Errorf("email = %v, want alice@example.com", got.Attributes["email"])
	}
	// Old attribute should be gone.
	if _, ok := got.Attributes["age"]; ok {
		t.Errorf("age should have been removed by upsert")
	}
	// Old relation should be gone.
	if knows := got.Relations["knows"]; len(knows) != 1 || knows[0] != "carol" {
		t.Errorf("knows = %v, want [carol]", knows)
	}
}

func TestBatchUpsertEntities(t *testing.T) {
	idx := tempIndex(t)

	// Add initial entities.
	initial := []*Entity{
		{ID: "alice", Type: "person", Attributes: map[string]any{"name": "Alice"}},
		{ID: "bob", Type: "person", Attributes: map[string]any{"name": "Bob"}},
	}
	for _, e := range initial {
		if err := idx.AddEntity(e); err != nil {
			t.Fatalf("AddEntity(%s): %v", e.ID, err)
		}
	}

	bi, ok := idx.(BatchIndexer)
	if !ok {
		t.Skip("index does not implement BatchIndexer")
	}

	// Batch upsert: update alice, add carol.
	batch := []*Entity{
		{ID: "alice", Type: "person", Attributes: map[string]any{"name": "Alice Updated"}},
		{ID: "carol", Type: "person", Attributes: map[string]any{"name": "Carol"}},
	}
	if err := bi.UpsertEntities(batch); err != nil {
		t.Fatalf("UpsertEntities: %v", err)
	}

	// Verify alice updated.
	got, err := idx.GetEntity("alice")
	if err != nil {
		t.Fatalf("GetEntity(alice): %v", err)
	}
	if got.Attributes["name"] != "Alice Updated" {
		t.Errorf("alice name = %v, want Alice Updated", got.Attributes["name"])
	}

	// Verify carol added.
	got, err = idx.GetEntity("carol")
	if err != nil {
		t.Fatalf("GetEntity(carol): %v", err)
	}
	if got.Attributes["name"] != "Carol" {
		t.Errorf("carol name = %v, want Carol", got.Attributes["name"])
	}

	// Verify bob unchanged.
	got, err = idx.GetEntity("bob")
	if err != nil {
		t.Fatalf("GetEntity(bob): %v", err)
	}
	if got.Attributes["name"] != "Bob" {
		t.Errorf("bob name = %v, want Bob", got.Attributes["name"])
	}
}

func TestBatchRemoveEntities(t *testing.T) {
	idx := tempIndex(t)

	entities := []*Entity{
		{ID: "alice", Type: "person", Relations: map[string][]string{"knows": {"bob"}}},
		{ID: "bob", Type: "person"},
		{ID: "carol", Type: "person"},
	}
	for _, e := range entities {
		if err := idx.AddEntity(e); err != nil {
			t.Fatalf("AddEntity(%s): %v", e.ID, err)
		}
	}

	bi, ok := idx.(BatchIndexer)
	if !ok {
		t.Skip("index does not implement BatchIndexer")
	}

	if err := bi.RemoveEntities([]string{"alice", "carol"}); err != nil {
		t.Fatalf("RemoveEntities: %v", err)
	}

	if _, err := idx.GetEntity("alice"); err == nil {
		t.Error("alice should have been removed")
	}
	if _, err := idx.GetEntity("carol"); err == nil {
		t.Error("carol should have been removed")
	}
	if _, err := idx.GetEntity("bob"); err != nil {
		t.Errorf("bob should still exist: %v", err)
	}
}

func TestBoltSimilarEntities(t *testing.T) {
	idx := tempIndex(t)

	// Create entities with embedding vectors.
	vec1 := []float64{1.0, 0.0, 0.0}
	vec2 := []float64{0.9, 0.1, 0.0} // similar to vec1
	vec3 := []float64{0.0, 0.0, 1.0} // orthogonal to vec1

	entities := []*Entity{
		{ID: "doc:a", Type: "doc", Attributes: map[string]any{"title": "Alpha", "title_embedding": vec1}},
		{ID: "doc:b", Type: "doc", Attributes: map[string]any{"title": "Beta", "title_embedding": vec2}},
		{ID: "doc:c", Type: "doc", Attributes: map[string]any{"title": "Gamma", "title_embedding": vec3}},
		{ID: "other:x", Type: "other", Attributes: map[string]any{"title": "X", "title_embedding": vec1}},
	}
	for _, e := range entities {
		if err := idx.AddEntity(e); err != nil {
			t.Fatalf("AddEntity(%s): %v", e.ID, err)
		}
	}

	vs, ok := idx.(VectorSearcher)
	if !ok {
		t.Fatal("boltIndex should implement VectorSearcher")
	}

	results, err := vs.SimilarEntities(SimilarityParams{
		TypeName: "doc",
		Vector:   vec1,
		Fields:   []string{"title"},
		TopN:     2,
	})
	if err != nil {
		t.Fatalf("SimilarEntities: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}

	// First result should be doc:a (exact match, score=1.0).
	if results[0].Entity.ID != "doc:a" {
		t.Errorf("results[0].ID = %s, want doc:a", results[0].Entity.ID)
	}
	if math.Abs(results[0].Score-1.0) > 0.01 {
		t.Errorf("results[0].Score = %f, want ~1.0", results[0].Score)
	}

	// Second result should be doc:b (similar, high score).
	if results[1].Entity.ID != "doc:b" {
		t.Errorf("results[1].ID = %s, want doc:b", results[1].Entity.ID)
	}
	if results[1].Score < 0.9 {
		t.Errorf("results[1].Score = %f, want > 0.9", results[1].Score)
	}

	// doc:c should not be in top 2 (orthogonal).
	// other:x should not appear (different type).
}

func TestBoltIndexStats(t *testing.T) {
idx := tempIndex(t)

entities := []*Entity{
{ID: "person:alice", Type: "person", Relations: map[string][]string{"works_at": {"company:acme"}}},
{ID: "person:bob", Type: "person", Relations: map[string][]string{"works_at": {"company:acme"}, "knows": {"person:alice"}}},
{ID: "company:acme", Type: "company"},
{ID: "tool:vim", Type: "tool"}, // orphan
}
for _, e := range entities {
if err := idx.AddEntity(e); err != nil {
t.Fatalf("AddEntity(%s): %v", e.ID, err)
}
}

sp, ok := idx.(StatsProvider)
if !ok {
t.Fatal("boltIndex should implement StatsProvider")
}

stats, err := sp.IndexStats()
if err != nil {
t.Fatalf("IndexStats: %v", err)
}

if stats.TotalEntities != 4 {
t.Errorf("TotalEntities = %d, want 4", stats.TotalEntities)
}
if stats.TotalLinks != 3 {
t.Errorf("TotalLinks = %d, want 3", stats.TotalLinks)
}
if stats.TypeCounts["person"] != 2 {
t.Errorf("TypeCounts[person] = %d, want 2", stats.TypeCounts["person"])
}
if stats.TypeCounts["company"] != 1 {
t.Errorf("TypeCounts[company] = %d, want 1", stats.TypeCounts["company"])
}
if stats.RelCounts["works_at"] != 2 {
t.Errorf("RelCounts[works_at] = %d, want 2", stats.RelCounts["works_at"])
}
if stats.RelCounts["knows"] != 1 {
t.Errorf("RelCounts[knows] = %d, want 1", stats.RelCounts["knows"])
}
if stats.Orphans != 1 {
t.Errorf("Orphans = %d, want 1 (tool:vim)", stats.Orphans)
}
}
