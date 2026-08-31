package store

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
	"testing"
	"time"

	"github.com/pbleisch/kg/pkg/vector"
)

// TestSidecarMatchesInFileVectors is the increment's own claim, made
// executable: searching the mmapped sidecar returns the same top-10, in the
// same order, with the same scores to four decimals, as searching the vectors
// where they used to live — inside the entity files.
//
// The reference implementation is the one the LadybugDB backend used before it
// was deleted: pull every entity of the type, resolve <field> or
// <field>_embedding out of its attributes, score it with CosineSimilarity, sort
// descending. It is reproduced here rather than cited, so this test keeps
// holding after the backend it came from is gone.
//
// The measurement on the real 27,081-vector corpus is in ADR-0026. This test
// pins the invariant that measurement depended on: the two paths agree.
func TestSidecarMatchesInFileVectors(t *testing.T) {
	const (
		entities = 1200
		dim      = 384
		topN     = 10
	)

	dir := t.TempDir()
	rng := rand.New(rand.NewSource(20260830))

	vecs := make([][]float64, entities)
	ids := make([]string, entities)
	for i := range entities {
		v := make([]float64, dim)
		for j := range v {
			v[j] = rng.NormFloat64()
		}
		vecs[i] = v
		ids[i] = fmt.Sprintf("game:e%05d", i)

		// The old layout: the vector is an attribute of the entity file.
		attrs := map[string]any{
			"description":           fmt.Sprintf("entity number %d", i),
			"description_embedding": toAny(v),
		}
		if err := WriteEntity(dir, &Entity{ID: ids[i], Type: "game", Attributes: attrs}); err != nil {
			t.Fatalf("WriteEntity: %v", err)
		}
	}

	// The query is a stored vector perturbed slightly, so the ranking is a
	// real ordering rather than one exact hit and 1,199 ties.
	query := make([]float64, dim)
	copy(query, vecs[42])
	for j := range query {
		query[j] += 0.35 * rng.NormFloat64()
	}

	// --- reference: score the vectors as they sit in the entity files ---
	refStart := time.Now()
	reference := referenceSearch(t, dir, "game", "description", query, topN)
	refElapsed := time.Since(refStart)

	// --- build the sidecar from the same vectors ---
	w := vector.NewWriter(CacheDir(dir), "sha256:test", dim)
	for i := range entities {
		if err := w.Put(vector.Entry{ID: ids[i], Type: "game", Field: "description"}, vecs[i]); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}
	if err := w.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	vs, err := vector.Open(CacheDir(dir), "sha256:test")
	if err != nil {
		t.Fatalf("Open sidecar: %v", err)
	}
	defer vs.Close()

	sideStart := time.Now()
	matches, err := vs.Search(vector.Query{Vector: query, Type: "game", Fields: []string{"description"}, TopN: topN})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	sideElapsed := time.Since(sideStart)

	// --- the comparison ---
	if len(matches) != len(reference) {
		t.Fatalf("sidecar returned %d matches, reference returned %d", len(matches), len(reference))
	}
	for i := range reference {
		if matches[i].ID != reference[i].id {
			t.Errorf("rank %d: sidecar %s, reference %s", i, matches[i].ID, reference[i].id)
			continue
		}
		gotR := round4(matches[i].Score)
		wantR := round4(reference[i].score)
		if gotR != wantR {
			t.Errorf("rank %d (%s): sidecar %.4f, reference %.4f", i, matches[i].ID, gotR, wantR)
		}
		// The scores are in fact bit-identical, because the sidecar stores the
		// same float64s and the search does the same arithmetic in the same
		// order. Four decimals is the claim; equality is the reality, and
		// asserting it catches a change of arithmetic that four decimals hides.
		if matches[i].Score != reference[i].score {
			t.Errorf("rank %d (%s): scores differ beyond four decimals: %v vs %v",
				i, matches[i].ID, matches[i].Score, reference[i].score)
		}
	}

	t.Logf("%d vectors of %d dims: sidecar %v, in-file reference %v (%.1fx)",
		entities, dim, sideElapsed.Round(time.Microsecond), refElapsed.Round(time.Microsecond),
		float64(refElapsed)/float64(sideElapsed))

	// A guard, not a benchmark. The sidecar scan is memory-bandwidth bound; if
	// it ever costs more than reading and parsing the YAML it was extracted
	// from, the reason for extracting it is gone.
	if sideElapsed > refElapsed {
		t.Errorf("sidecar search (%v) was slower than parsing the entity files (%v)", sideElapsed, refElapsed)
	}
}

type refMatch struct {
	id    string
	score float64
}

// referenceSearch reproduces the pre-sidecar search: read every entity of the
// type, take the vector out of its attributes, score it, sort.
func referenceSearch(t *testing.T, kgDir, typeName, field string, query []float64, topN int) []refMatch {
	t.Helper()
	entities, err := ListEntitiesByType(kgDir, typeName)
	if err != nil {
		t.Fatalf("ListEntitiesByType: %v", err)
	}
	var out []refMatch
	for _, e := range entities {
		_, vec := ResolveVector(e.Attributes, field)
		if len(vec) == 0 {
			continue
		}
		score, err := CosineSimilarity(query, vec)
		if err != nil {
			continue
		}
		out = append(out, refMatch{id: e.ID, score: score})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score > out[j].score
		}
		return out[i].id < out[j].id
	})
	if topN > 0 && len(out) > topN {
		out = out[:topN]
	}
	return out
}

func toAny(v []float64) []any {
	out := make([]any, len(v))
	for i, f := range v {
		out[i] = f
	}
	return out
}

func round4(f float64) float64 { return math.Round(f*1e4) / 1e4 }
