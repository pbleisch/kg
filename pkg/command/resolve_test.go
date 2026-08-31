package command

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pbleisch/kg/pkg/store"
	"github.com/pbleisch/kg/pkg/vector"
)

// resolveGraph writes entities and a sidecar holding exactly the vectors given.
func resolveGraph(t *testing.T, rows []struct {
	id, typ, name string
	vec           []float64
}, embed map[string]bool) string {
	t.Helper()
	dir := t.TempDir()
	w := vector.NewWriter(store.CacheDir(dir), "", len(rows[0].vec))
	for _, r := range rows {
		e := &store.Entity{ID: r.id, Type: r.typ, Attributes: map[string]any{"name": r.name}}
		if err := store.WriteEntity(dir, e); err != nil {
			t.Fatalf("WriteEntity(%s): %v", r.id, err)
		}
		if embed != nil && !embed[r.id] {
			continue
		}
		if err := w.Put(vector.Entry{ID: r.id, Type: r.typ, Field: "name"}, r.vec); err != nil {
			t.Fatalf("Put(%s): %v", r.id, err)
		}
	}
	if err := w.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	return dir
}

type resolveRow = struct {
	id, typ, name string
	vec           []float64
}

func TestResolve_FindsPairsAcrossTypes(t *testing.T) {
	dir := resolveGraph(t, []resolveRow{
		{"concept:match_three", "concept", "Match-Three", []float64{1, 0, 0}},
		{"genre:match_three", "genre", "Match-Three", []float64{1, 0, 0}},
		{"concept:matching", "concept", "Matching", []float64{0.99, 0.14, 0}},
		{"person:alice", "person", "Alice", []float64{0, 1, 0}},
	}, nil)

	res, err := Resolve(dir, ResolveOptions{Threshold: 0.90, Fields: []string{"name"}})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Total != 3 {
		t.Fatalf("Total = %d, want 3: %+v", res.Total, res.Pairs)
	}
	// concept~genre and genre~concept:matching cross the type line; only
	// concept:match_three ~ concept:matching stays inside one type.
	if res.SameType != 1 || res.CrossType != 2 {
		t.Errorf("same/cross = %d/%d, want 1/2", res.SameType, res.CrossType)
	}
	// Highest score first, and the cross-type pair is the identical one — the
	// pair `--dedup-fields name` on one type could never have reached.
	top := res.Pairs[0]
	if top.A != "concept:match_three" || top.B != "genre:match_three" {
		t.Errorf("top pair = %s ~ %s, want the cross-type identical names", top.A, top.B)
	}
	if top.TextA != "Match-Three" || top.TextB != "Match-Three" {
		t.Errorf("the pair does not carry the text a human adjudicates: %+v", top)
	}
}

func TestResolve_TypeFilters(t *testing.T) {
	rows := []resolveRow{
		{"concept:a", "concept", "A", []float64{1, 0, 0}},
		{"genre:a", "genre", "A", []float64{1, 0, 0}},
		{"concept:b", "concept", "B", []float64{1, 0, 0}},
	}
	dir := resolveGraph(t, rows, nil)

	same, err := Resolve(dir, ResolveOptions{Fields: []string{"name"}, SameType: true})
	if err != nil {
		t.Fatal(err)
	}
	if same.Total != 1 || same.CrossType != 0 {
		t.Errorf("--same-type gave %+v", same.Pairs)
	}
	cross, err := Resolve(dir, ResolveOptions{Fields: []string{"name"}, CrossType: true})
	if err != nil {
		t.Fatal(err)
	}
	if cross.Total != 2 || cross.SameType != 0 {
		t.Errorf("--cross-type gave %+v", cross.Pairs)
	}
}

// A partial index is refused rather than answered from. The pass this replaced
// resolved against a sidecar that `kg sync` refreshed after the apply, so it
// was permanently one document behind.
func TestResolve_RefusesAnIncompleteIndex(t *testing.T) {
	dir := resolveGraph(t, []resolveRow{
		{"concept:a", "concept", "A", []float64{1, 0, 0}},
		{"concept:b", "concept", "B", []float64{1, 0, 0}},
		{"concept:c", "concept", "C", []float64{0, 1, 0}},
	}, map[string]bool{"concept:a": true, "concept:b": true})

	_, err := Resolve(dir, ResolveOptions{Fields: []string{"name"}})
	if err == nil {
		t.Fatal("Resolve answered from a sidecar missing a vector")
	}
	if want := "concept:c.name"; !strings.Contains(err.Error(), want) {
		t.Errorf("error = %v, want it to name %q", err, want)
	}
}

// A corpus that does not load is refused by the load, before any comparison.
func TestResolve_RefusesACorpusThatDoesNotLoad(t *testing.T) {
	dir := resolveGraph(t, []resolveRow{
		{"concept:a", "concept", "A", []float64{1, 0, 0}},
		{"concept:b", "concept", "B", []float64{1, 0, 0}},
	}, nil)
	stray := filepath.Join(dir, "entities", "genre", "a.yaml")
	if err := os.MkdirAll(filepath.Dir(stray), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stray, []byte("id: concept:a\ntype: concept\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Resolve(dir, ResolveOptions{Fields: []string{"name"}})
	if _, refused := store.LoadRefused(err); !refused {
		t.Fatalf("Resolve err = %v, want a *store.LoadError", err)
	}
}

// The same corpus answers with the same pairs in the same order (P6).
func TestResolve_IsDeterministic(t *testing.T) {
	dir := resolveGraph(t, []resolveRow{
		{"concept:c", "concept", "C", []float64{1, 0, 0}},
		{"concept:a", "concept", "A", []float64{1, 0, 0}},
		{"concept:b", "concept", "B", []float64{1, 0, 0}},
		{"genre:a", "genre", "A", []float64{1, 0, 0}},
	}, nil)

	first, err := Resolve(dir, ResolveOptions{Fields: []string{"name"}})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		again, err := Resolve(dir, ResolveOptions{Fields: []string{"name"}})
		if err != nil {
			t.Fatal(err)
		}
		if len(again.Pairs) != len(first.Pairs) {
			t.Fatalf("run %d found %d pairs, first found %d", i, len(again.Pairs), len(first.Pairs))
		}
		for j := range first.Pairs {
			if first.Pairs[j] != again.Pairs[j] {
				t.Fatalf("run %d pair %d differs:\n %+v\n %+v", i, j, first.Pairs[j], again.Pairs[j])
			}
		}
	}
}

// Resolve reports; it never merges.
func TestResolve_WritesNothing(t *testing.T) {
	dir := resolveGraph(t, []resolveRow{
		{"concept:a", "concept", "A", []float64{1, 0, 0}},
		{"concept:b", "concept", "A", []float64{1, 0, 0}},
	}, nil)

	before := snapshot(t, dir)
	if _, err := Resolve(dir, ResolveOptions{Fields: []string{"name"}}); err != nil {
		t.Fatal(err)
	}
	after := snapshot(t, dir)
	if len(before) != len(after) {
		t.Fatalf("Resolve changed the corpus: %d files before, %d after", len(before), len(after))
	}
	for path, sum := range before {
		if after[path] != sum {
			t.Errorf("Resolve rewrote %s", path)
		}
	}
}

func snapshot(t *testing.T, kgDir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	files, err := store.ListEntityFiles(kgDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		data, err := os.ReadFile(f.Path)
		if err != nil {
			t.Fatal(err)
		}
		out[f.Path] = string(data)
	}
	return out
}
