package vector

import (
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
)

// build writes a sidecar of n random vectors of the given dimension and
// returns the cache dir plus the vectors, keyed by row.
func build(t *testing.T, n, dim int, embedder string) (string, []Entry, [][]float64) {
	t.Helper()
	dir := t.TempDir()
	w := NewWriter(dir, embedder, dim)
	rng := rand.New(rand.NewSource(1))

	entries := make([]Entry, n)
	vecs := make([][]float64, n)
	for i := range n {
		v := make([]float64, dim)
		for j := range v {
			v[j] = rng.NormFloat64()
		}
		e := Entry{
			ID:       idFor(i),
			Type:     typeFor(i),
			Field:    "description",
			Checksum: "cksum-" + idFor(i),
		}
		entries[i] = e
		vecs[i] = v
		if err := w.Put(e, v); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}
	if err := w.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	return dir, entries, vecs
}

func idFor(i int) string {
	if i%3 == 0 {
		return "game:e" + pad(i)
	}
	return "concept:e" + pad(i)
}

func typeFor(i int) string {
	if i%3 == 0 {
		return "game"
	}
	return "concept"
}

func pad(i int) string {
	s := ""
	for v := i; v > 0; v /= 10 {
		s = string(rune('0'+v%10)) + s
	}
	if s == "" {
		s = "0"
	}
	for len(s) < 6 {
		s = "0" + s
	}
	return s
}

func TestRoundTrip(t *testing.T) {
	dir, entries, vecs := build(t, 50, 16, "sha256:abc")

	s, err := Open(dir, "sha256:abc")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	if s.Dim() != 16 {
		t.Errorf("Dim = %d, want 16", s.Dim())
	}
	if s.Len() != 50 {
		t.Errorf("Len = %d, want 50", s.Len())
	}
	for i, e := range entries {
		got, ok := s.Vector(e.ID, e.Field)
		if !ok {
			t.Fatalf("Vector(%s) missing", e.ID)
		}
		for j := range got {
			if got[j] != vecs[i][j] {
				t.Fatalf("%s[%d] = %v, want %v", e.ID, j, got[j], vecs[i][j])
			}
		}
	}
}

// TestRowsAreDeterministic pins the property the format exists for: the same
// set of vectors always produces the same bytes, whatever order they arrive in.
func TestRowsAreDeterministic(t *testing.T) {
	dim := 8
	mk := func(reverse bool) []byte {
		dir := t.TempDir()
		w := NewWriter(dir, "e", dim)
		idx := []int{0, 1, 2, 3, 4}
		if reverse {
			idx = []int{4, 3, 2, 1, 0}
		}
		for _, i := range idx {
			v := make([]float64, dim)
			for j := range v {
				v[j] = float64(i*dim + j)
			}
			if err := w.Put(Entry{ID: idFor(i), Type: typeFor(i), Field: "description"}, v); err != nil {
				t.Fatalf("Put: %v", err)
			}
		}
		if err := w.Commit(); err != nil {
			t.Fatalf("Commit: %v", err)
		}
		b, err := os.ReadFile(BinPath(dir))
		if err != nil {
			t.Fatalf("read bin: %v", err)
		}
		return b
	}
	forward, backward := mk(false), mk(true)
	if len(forward) != len(backward) {
		t.Fatalf("bin sizes differ: %d vs %d", len(forward), len(backward))
	}
	for i := range forward {
		if forward[i] != backward[i] {
			t.Fatalf("bin differs at byte %d: insertion order leaked into the file", i)
		}
	}
}

func TestPutReplaces(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir, "e", 4)
	e := Entry{ID: "game:a", Type: "game", Field: "description", Checksum: "one"}
	if err := w.Put(e, []float64{1, 1, 1, 1}); err != nil {
		t.Fatal(err)
	}
	e.Checksum = "two"
	if err := w.Put(e, []float64{2, 2, 2, 2}); err != nil {
		t.Fatal(err)
	}
	if w.Len() != 1 {
		t.Fatalf("Len = %d, want 1 after replacing the same pair", w.Len())
	}
	if err := w.Commit(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir, "e")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, _ := s.Vector("game:a", "description")
	if got[0] != 2 {
		t.Errorf("vector = %v, want the second Put to win", got)
	}
	if ent, _ := s.Index().Lookup("game:a", "description"); ent.Checksum != "two" {
		t.Errorf("checksum = %q, want %q", ent.Checksum, "two")
	}
}

func TestDrop(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir, "e", 2)
	for _, id := range []string{"game:a", "game:b", "game:c"} {
		if err := w.Put(Entry{ID: id, Type: "game", Field: "description"}, []float64{1, 2}); err != nil {
			t.Fatal(err)
		}
	}
	w.Drop("game:b", "description")
	if w.Len() != 2 {
		t.Fatalf("Len = %d, want 2", w.Len())
	}
	if w.Has("game:b", "description") {
		t.Error("dropped pair still staged")
	}
	if !w.Has("game:c", "description") {
		t.Error("Drop disturbed an unrelated pair")
	}
	if err := w.Commit(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir, "e")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, ok := s.Vector("game:b", "description"); ok {
		t.Error("dropped pair survived Commit")
	}
	if _, ok := s.Vector("game:c", "description"); !ok {
		t.Error("Commit lost an unrelated pair")
	}
}

func TestSearchFiltersAndRanks(t *testing.T) {
	dir, entries, vecs := build(t, 200, 32, "e")
	s, err := Open(dir, "e")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// Query with a stored vector: it must rank itself first at score 1.
	q := vecs[7]
	got, err := s.Search(Query{Vector: q, TopN: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Fatalf("got %d matches, want 5", len(got))
	}
	if got[0].ID != entries[7].ID {
		t.Errorf("top match = %s, want %s", got[0].ID, entries[7].ID)
	}
	if math.Abs(got[0].Score-1.0) > 1e-12 {
		t.Errorf("self-similarity = %v, want 1", got[0].Score)
	}
	for i := 1; i < len(got); i++ {
		if got[i].Score > got[i-1].Score {
			t.Fatalf("results not sorted: %v then %v", got[i-1].Score, got[i].Score)
		}
	}

	// Type filter.
	byType, err := s.Search(Query{Vector: q, Type: "genre", TopN: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(byType) != 0 {
		t.Errorf("got %d matches for an absent type, want 0", len(byType))
	}
	games, err := s.Search(Query{Vector: q, Type: "game"})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range games {
		if m.Type != "game" {
			t.Fatalf("type filter leaked a %s", m.Type)
		}
	}

	// Field filter, including the legacy attribute spelling.
	legacy, err := s.Search(Query{Vector: q, Fields: []string{"description_embedding"}, TopN: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(legacy) != 3 {
		t.Errorf("legacy field spelling matched %d, want 3", len(legacy))
	}
	none, err := s.Search(Query{Vector: q, Fields: []string{"nosuchfield"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 0 {
		t.Errorf("got %d matches for an absent field, want 0", len(none))
	}
}

func TestSearchDimensionMismatch(t *testing.T) {
	dir, _, _ := build(t, 5, 8, "e")
	s, err := Open(dir, "e")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.Search(Query{Vector: []float64{1, 2, 3}}); err == nil {
		t.Error("Search accepted a 3-dim query against an 8-dim sidecar")
	}
}

// TestStaleEmbedderIsRefused is the property that makes changing the embedding
// model safe: the sidecar is refused rather than searched with scores that
// cannot be compared.
func TestStaleEmbedderIsRefused(t *testing.T) {
	dir, _, _ := build(t, 5, 8, "sha256:model-a")
	if _, err := Open(dir, "sha256:model-b"); err == nil {
		t.Fatal("Open accepted a sidecar built by a different embedder")
	}
	if _, err := Open(dir, "sha256:model-a"); err != nil {
		t.Fatalf("Open rejected a sidecar built by the same embedder: %v", err)
	}
	// An unstated embedder reads whatever is there: callers that do not know
	// which embedder they want (`kg similar --vector`) are not blocked.
	if _, err := Open(dir, ""); err != nil {
		t.Fatalf("Open rejected a sidecar for an unstated embedder: %v", err)
	}
}

func TestMissingSidecar(t *testing.T) {
	dir := t.TempDir()
	_, err := Open(dir, "")
	var missing *ErrNoSidecar
	if err == nil {
		t.Fatal("Open succeeded on an empty directory")
	}
	if !asErrNoSidecar(err, &missing) {
		t.Fatalf("Open returned %v, want *ErrNoSidecar", err)
	}
}

func TestTruncatedBinIsRefused(t *testing.T) {
	dir, _, _ := build(t, 10, 8, "e")
	// Lop off the last row: the index still claims it.
	b, err := os.ReadFile(BinPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(BinPath(dir), b[:len(b)-64], 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir, "e"); err == nil {
		t.Error("Open accepted a bin file shorter than the index claims")
	}
}

func TestRemove(t *testing.T) {
	dir, _, _ := build(t, 3, 4, "e")
	if err := Remove(dir); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	for _, p := range []string{BinPath(dir), IdxPath(dir)} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s survived Remove", filepath.Base(p))
		}
	}
	if err := Remove(dir); err != nil {
		t.Errorf("Remove on an already-removed sidecar: %v", err)
	}
}

func asErrNoSidecar(err error, target **ErrNoSidecar) bool {
	e, ok := err.(*ErrNoSidecar)
	if ok {
		*target = e
	}
	return ok
}
