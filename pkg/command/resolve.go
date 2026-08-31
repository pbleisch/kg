package command

import (
	"fmt"
	"math"
	"runtime"
	"sort"
	"sync"

	"github.com/pbleisch/kg/pkg/store"
	"github.com/pbleisch/kg/pkg/vector"
)

// ResolveOptions configures a whole-corpus entity-resolution pass.
type ResolveOptions struct {
	// Threshold is the cosine score at or above which a pair is a candidate.
	// Zero means 0.90.
	Threshold float64
	// Fields are the embedded fields to compare. Empty means every field the
	// configured embedder covers, and failing that every field in the sidecar.
	Fields []string
	// SameType and CrossType select which pairs are reported. Both false means
	// both — the useful default, and the one `--dedup-fields name` could never
	// reach: a concept and a source with the same name were never compared.
	SameType  bool
	CrossType bool
	// Limit caps the reported pairs, highest score first. Zero means all.
	Limit int
}

// A Candidate is two entities a human should look at.  It is not a decision:
// whether two names are the same thing is judgement, and kg does not merge on
// a threshold.
type Candidate struct {
	A     string  `json:"a"`
	B     string  `json:"b"`
	TypeA string  `json:"type_a"`
	TypeB string  `json:"type_b"`
	TextA string  `json:"text_a,omitempty"`
	TextB string  `json:"text_b,omitempty"`
	Field string  `json:"field"`
	Score float64 `json:"score"`
}

// A ResolveResult is one pass's answer.
type ResolveResult struct {
	Entities  int         `json:"entities"`
	Compared  int         `json:"compared"`
	Threshold float64     `json:"threshold"`
	Fields    []string    `json:"fields"`
	SameType  int         `json:"same_type"`
	CrossType int         `json:"cross_type"`
	Total     int         `json:"total"`
	Pairs     []Candidate `json:"pairs"`
}

// Resolve finds every pair of entities whose embedded text is close enough to
// be the same thing, over the whole corpus, in one pass.
//
// **It runs against a complete index or not at all.** Resolution used to happen
// inside the write boundary, once per document, against a vector sidecar that
// `kg sync` refreshed *after* the apply — so it was permanently one document
// behind, and the pairs it could not see were the ones it had just created.
// This refuses rather than answering from a partial index: a graph that does
// not load is refused by the load, and an entity whose text has no vector is
// counted and named before anything is compared.
//
// **It never merges.** Deciding that two names are one thing is adjudication.
// The output is the list a person or an agent adjudicates; `kg merge` applies
// what they decide.
func Resolve(kgDir string, opts ResolveOptions) (*ResolveResult, error) {
	if opts.Threshold == 0 {
		opts.Threshold = 0.90
	}
	if opts.Threshold <= 0 || opts.Threshold > 1 {
		return nil, fmt.Errorf("resolve: threshold %v is outside 0.0–1.0", opts.Threshold)
	}
	if !opts.SameType && !opts.CrossType {
		opts.SameType, opts.CrossType = true, true
	}

	// The load refuses a corpus that breaks P3, which is what makes the index
	// "complete" a meaningful claim: an identity two files claim has one vector
	// row for two entities, and resolving against that is resolving against a
	// corpus that is not the one on disk.
	g, err := store.LoadGraph(kgDir)
	if err != nil {
		return nil, err
	}

	digest := ""
	fields := opts.Fields
	if emb, err := store.ResolveEmbedder(kgDir); err == nil {
		digest = emb.Digest()
		if len(fields) == 0 {
			fields = emb.Fields
		}
	}
	sidecar, err := vector.Open(store.CacheDir(kgDir), digest)
	if err != nil {
		return nil, err
	}
	defer sidecar.Close()

	if len(fields) == 0 {
		seen := map[string]bool{}
		for _, e := range sidecar.Index().Entries {
			seen[e.Field] = true
		}
		for f := range seen {
			fields = append(fields, f)
		}
	}
	sort.Strings(fields)
	if len(fields) == 0 {
		return nil, fmt.Errorf("resolve: no fields to compare; name them with --fields or configure an embedder")
	}

	res := &ResolveResult{Entities: g.Len(), Threshold: opts.Threshold, Fields: fields}

	// One row per (entity, field) that has both text and a vector, and a count
	// of the ones that have text and no vector. The second number is the whole
	// completeness check.
	type row struct {
		id, typ, field, text string
		vec                  []float64
	}
	var rows []row
	var named []string
	behind := 0
	for _, e := range g.Entities() {
		for _, f := range fields {
			text, ok := e.Attributes[f]
			if !ok {
				continue
			}
			s := fmt.Sprintf("%v", text)
			if s == "" {
				continue
			}
			vec, ok := sidecar.Vector(e.ID, f)
			if !ok || len(vec) == 0 {
				behind++
				if len(named) < 5 {
					named = append(named, e.ID+"."+f)
				}
				continue
			}
			rows = append(rows, row{e.ID, e.Type, f, s, vec})
		}
	}
	if behind > 0 {
		return nil, fmt.Errorf(
			"resolve: the vector sidecar is behind the corpus — %d (entity, field) pair(s) have text and no vector, starting with %v; run `kg sync` and try again",
			behind, named)
	}
	res.Compared = len(rows)
	if len(rows) < 2 {
		return res, nil
	}

	// Group by field: comparing one entity's name against another's
	// description is a category error, not a low score.
	byField := map[string][]int{}
	for i, r := range rows {
		byField[r.field] = append(byField[r.field], i)
	}

	dim := sidecar.Dim()
	best := map[[2]string]Candidate{}
	for _, f := range fields {
		idx := byField[f]
		if len(idx) < 2 {
			continue
		}
		vecs := make([][]float64, len(idx))
		for k, i := range idx {
			vecs[k] = rows[i].vec
		}
		for _, p := range allPairs(normalize(vecs, dim), dim, float32(opts.Threshold)) {
			a, b := rows[idx[p.i]], rows[idx[p.j]]
			if a.id == b.id {
				continue
			}
			same := a.typ == b.typ
			if same && !opts.SameType {
				continue
			}
			if !same && !opts.CrossType {
				continue
			}
			key := [2]string{a.id, b.id}
			if key[0] > key[1] {
				key = [2]string{b.id, a.id}
				a, b = b, a
			}
			c := Candidate{
				A: a.id, B: b.id, TypeA: a.typ, TypeB: b.typ,
				TextA: a.text, TextB: b.text, Field: f, Score: float64(p.score),
			}
			if prev, ok := best[key]; ok && prev.Score >= c.Score {
				continue
			}
			best[key] = c
		}
	}

	res.Pairs = make([]Candidate, 0, len(best))
	for _, c := range best {
		res.Pairs = append(res.Pairs, c)
	}
	// Highest score first, then by identity: the same corpus answers with the
	// same bytes in the same order (P6).
	sort.Slice(res.Pairs, func(i, j int) bool {
		a, b := res.Pairs[i], res.Pairs[j]
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		if a.A != b.A {
			return a.A < b.A
		}
		if a.B != b.B {
			return a.B < b.B
		}
		return a.Field < b.Field
	})
	for _, c := range res.Pairs {
		if c.TypeA == c.TypeB {
			res.SameType++
		} else {
			res.CrossType++
		}
	}
	res.Total = len(res.Pairs)
	if opts.Limit > 0 && len(res.Pairs) > opts.Limit {
		res.Pairs = res.Pairs[:opts.Limit]
	}
	return res, nil
}

// pair is one above-threshold comparison, in slab indices.
type pair struct {
	i, j  int
	score float32
}

// allPairs compares every row against every later row and returns the ones at
// or above thr.
//
// It is O(n²) and deliberately so: at the corpus ceiling ADR-0026 states, the
// exact answer costs a couple of seconds and an approximate index costs a
// derived artifact to keep, an accuracy argument, and a second thing that can
// go stale (P2). Measured on the 27k-entity corpus, one field: 2.6 s.
func allPairs(slab []float32, dim int, thr float32) []pair {
	n := len(slab) / dim
	workers := runtime.GOMAXPROCS(0)
	out := make([][]pair, workers)
	next := make(chan int, workers*8)
	go func() {
		for i := 0; i < n; i++ {
			next <- i
		}
		close(next)
	}()
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := range next {
				a := slab[i*dim : i*dim+dim : i*dim+dim]
				for j := i + 1; j < n; j++ {
					b := slab[j*dim : j*dim+dim : j*dim+dim]
					if s := dot(a, b); s >= thr {
						out[w] = append(out[w], pair{i, j, s})
					}
				}
			}
		}(w)
	}
	wg.Wait()
	var all []pair
	for _, o := range out {
		all = append(all, o...)
	}
	return all
}

// dot is the inner loop. Four accumulators because Go emits no SIMD here and
// the dependency chain is what costs.
func dot(a, b []float32) float32 {
	var s0, s1, s2, s3 float32
	i := 0
	for ; i+4 <= len(a); i += 4 {
		s0 += a[i] * b[i]
		s1 += a[i+1] * b[i+1]
		s2 += a[i+2] * b[i+2]
		s3 += a[i+3] * b[i+3]
	}
	for ; i < len(a); i++ {
		s0 += a[i] * b[i]
	}
	return s0 + s1 + s2 + s3
}

// normalize packs vectors into one unit-length float32 slab.
//
// float32 rather than float64: the pass is memory-bound, and half the bytes is
// most of the speed. Unit length once rather than per pair, so the inner loop
// is a dot product and nothing else.
func normalize(vecs [][]float64, dim int) []float32 {
	slab := make([]float32, len(vecs)*dim)
	for i, v := range vecs {
		var n float64
		for _, x := range v {
			n += x * x
		}
		n = math.Sqrt(n)
		if n == 0 {
			n = 1
		}
		off := i * dim
		for j := 0; j < dim && j < len(v); j++ {
			slab[off+j] = float32(v[j] / n)
		}
	}
	return slab
}
