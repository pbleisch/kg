package store

import (
	"os"
	"path/filepath"
	"testing"
)

func seedGraph(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	entities := []*Entity{
		{ID: "person:alice", Type: "person", Attributes: map[string]any{"role": "engineer"},
			Relations: map[string][]string{"works_on": {"project:atlas"}, "mentors": {"person:bob"}}},
		{ID: "person:bob", Type: "person", Attributes: map[string]any{"role": "designer"},
			Relations: map[string][]string{"works_on": {"project:atlas"}}},
		{ID: "person:carol", Type: "person", Attributes: map[string]any{"role": "engineer"}},
		{ID: "project:atlas", Type: "project", Attributes: map[string]any{"status": "active"}},
	}
	for _, e := range entities {
		if err := WriteEntity(dir, e); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestLoadGraph(t *testing.T) {
	g, err := LoadGraph(seedGraph(t))
	if err != nil {
		t.Fatalf("LoadGraph: %v", err)
	}
	if g.Len() != 4 {
		t.Errorf("Len = %d, want 4", g.Len())
	}
	if _, ok := g.Entity("person:alice"); !ok {
		t.Error("person:alice missing")
	}
	if _, ok := g.Entity("person:nobody"); ok {
		t.Error("an absent entity was found")
	}
	if got := g.Types(); len(got) != 2 || got[0] != "person" || got[1] != "project" {
		t.Errorf("Types = %v, want [person project]", got)
	}
	ids := g.IDsByType("person")
	if len(ids) != 3 || ids[0] != "person:alice" {
		t.Errorf("IDsByType(person) = %v, want three sorted ids", ids)
	}
}

func TestLoadGraph_Empty(t *testing.T) {
	g, err := LoadGraph(t.TempDir())
	if err != nil {
		t.Fatalf("LoadGraph on a graph with no entities: %v", err)
	}
	if g.Len() != 0 {
		t.Errorf("Len = %d, want 0", g.Len())
	}
}

// A file that will not parse refuses the load, naming it. It used to be
// skipped with a warning; a file kg cannot read is a fact it cannot reach, so
// it is structural (ADR-0028).
func TestLoadGraph_RefusesMalformed(t *testing.T) {
	dir := seedGraph(t)
	bad := filepath.Join(dir, "entities", "person", "broken.yaml")
	if err := os.WriteFile(bad, []byte("id: [unclosed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadGraph(dir)
	errs, refused := LoadRefused(err)
	if !refused {
		t.Fatalf("LoadGraph err = %v, want a *LoadError", err)
	}
	if len(errs) != 1 || errs[0].Code != "malformed" {
		t.Fatalf("errors = %+v, want one malformed", errs)
	}
	if len(errs[0].Files) != 1 || errs[0].Files[0] != bad {
		t.Errorf("Files = %v, want %q", errs[0].Files, bad)
	}
}

func TestGraph_Referrers(t *testing.T) {
	g, err := LoadGraph(seedGraph(t))
	if err != nil {
		t.Fatal(err)
	}
	via := g.ReferrersVia("works_on", "project:atlas")
	if len(via) != 2 || via[0] != "person:alice" || via[1] != "person:bob" {
		t.Errorf("ReferrersVia = %v, want [person:alice person:bob]", via)
	}
	if got := g.ReferrersVia("mentors", "project:atlas"); len(got) != 0 {
		t.Errorf("ReferrersVia with the wrong relation = %v, want none", got)
	}
	all := g.Referrers("person:bob")
	if len(all) != 1 || all[0] != "person:alice" {
		t.Errorf("Referrers(person:bob) = %v, want [person:alice]", all)
	}
	if got := g.Referrers("person:carol"); len(got) != 0 {
		t.Errorf("Referrers on an unreferenced entity = %v, want none", got)
	}
}

func TestGraph_FindEntities(t *testing.T) {
	g, err := LoadGraph(seedGraph(t))
	if err != nil {
		t.Fatal(err)
	}
	engineers, err := g.FindEntities("person", QueryOptions{
		Filters: []AttrFilter{{Attr: "role", Op: "=", Value: "engineer"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(engineers) != 2 {
		t.Fatalf("got %d engineers, want 2", len(engineers))
	}
	if engineers[0].Relations != nil {
		t.Error("relations were included without IncludeRelations")
	}

	page, err := g.FindEntities("person", QueryOptions{Offset: 1, Limit: 1, IncludeRelations: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 1 || page[0].ID != "person:bob" {
		t.Errorf("paged result = %v, want person:bob", page)
	}
	if page[0].Relations == nil {
		t.Error("IncludeRelations was ignored")
	}
}

// A projection must not reach back into the graph and mutate it.
func TestGraph_FindEntitiesCopies(t *testing.T) {
	g, err := LoadGraph(seedGraph(t))
	if err != nil {
		t.Fatal(err)
	}
	got, err := g.FindEntities("person", QueryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got[0].Attributes["role"] = "mutated"
	e, _ := g.Entity(got[0].ID)
	if e.Attributes["role"] == "mutated" {
		t.Error("a read mutated the graph")
	}
}

func TestGraph_StripsEmbeddingsByDefault(t *testing.T) {
	dir := t.TempDir()
	if err := WriteEntity(dir, &Entity{ID: "person:alice", Type: "person", Attributes: map[string]any{
		"name":           "Alice",
		"name_embedding": []any{0.1, 0.2},
	}}); err != nil {
		t.Fatal(err)
	}
	g, err := LoadGraph(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := g.FindEntities("person", QueryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got[0].Attributes["name_embedding"]; ok {
		t.Error("a legacy embedding attribute leaked into a read")
	}
	got, err = g.FindEntities("person", QueryOptions{IncludeEmbeddings: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got[0].Attributes["name_embedding"]; !ok {
		t.Error("IncludeEmbeddings did not include it")
	}
}

func TestGraph_Stats(t *testing.T) {
	g, err := LoadGraph(seedGraph(t))
	if err != nil {
		t.Fatal(err)
	}
	s := g.Stats()
	if s.TotalEntities != 4 {
		t.Errorf("TotalEntities = %d, want 4", s.TotalEntities)
	}
	if s.TotalLinks != 3 {
		t.Errorf("TotalLinks = %d, want 3", s.TotalLinks)
	}
	if s.TypeCounts["person"] != 3 || s.TypeCounts["project"] != 1 {
		t.Errorf("TypeCounts = %v", s.TypeCounts)
	}
	if s.RelCounts["works_on"] != 2 {
		t.Errorf("RelCounts[works_on] = %d, want 2", s.RelCounts["works_on"])
	}
	if s.Orphans != 1 {
		t.Errorf("Orphans = %d, want 1 (carol)", s.Orphans)
	}
}

func TestLoader(t *testing.T) {
	dir := seedGraph(t)
	l := NewLoader(dir)

	if got := l.Relations("person:alice", "works_on"); len(got) != 1 || got[0] != "project:atlas" {
		t.Errorf("Relations = %v", got)
	}
	// A missing entity is a miss, not an error: a dangling target must not
	// blow up a traversal.
	if _, ok := l.Entity("person:nobody"); ok {
		t.Error("Loader found an absent entity")
	}
	if got := l.Relations("person:nobody", "works_on"); got != nil {
		t.Errorf("Relations on an absent entity = %v, want nil", got)
	}

	// The miss is cached: deleting the file underneath must not change a
	// second answer within one traversal.
	if err := os.Remove(EntityPath(dir, "person:alice")); err != nil {
		t.Fatal(err)
	}
	if got := l.Relations("person:alice", "works_on"); len(got) != 1 {
		t.Errorf("a cached entity was re-read mid-traversal: %v", got)
	}
}

func TestFindByType(t *testing.T) {
	dir := seedGraph(t)
	got, err := FindByType(dir, "person", QueryOptions{IncludeRelations: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d, want 3", len(got))
	}
	if got[0].ID != "person:alice" || got[2].ID != "person:carol" {
		t.Errorf("results are not sorted by id: %v", []string{got[0].ID, got[2].ID})
	}
	empty, err := FindByType(dir, "nosuchtype", QueryOptions{})
	if err != nil {
		t.Errorf("an absent type should be an empty result, not an error: %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("got %d results for an absent type", len(empty))
	}
}
