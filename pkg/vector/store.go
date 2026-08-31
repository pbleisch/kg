package vector

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
)

// Store is a read-only view of the sidecar: the index plus the mmapped floats.
// It is safe for concurrent reads and must be closed to release the mapping.
type Store struct {
	idx    *Index
	floats []float64
	unmap  func() error
	// byKey indexes Entries so a caller probing every entity in a corpus
	// does not turn one pass into a quadratic one.
	byKey map[[2]string]Entry
}

// ErrNoSidecar reports that a graph has no vector sidecar. Callers decide
// whether that is fatal; for `kg similar` it is, and the message says to run
// `kg sync`.
type ErrNoSidecar struct{ Dir string }

func (e *ErrNoSidecar) Error() string {
	return fmt.Sprintf("no vector sidecar in %s: run `kg sync` to build one", e.Dir)
}

// Open reads the sidecar in cacheDir. It returns *ErrNoSidecar when the pair
// is absent, and an ordinary error when it is present but unreadable.
//
// embedder, when non-empty, is the digest of the embedding command the caller
// intends to use; a sidecar built by a different embedder is reported stale
// rather than searched, because scores across two embedders are not comparable.
func Open(cacheDir, embedder string) (*Store, error) {
	idx, err := LoadIndex(cacheDir)
	if err != nil {
		return nil, err
	}
	if len(idx.Entries) == 0 && idx.Dim == 0 {
		return nil, &ErrNoSidecar{Dir: cacheDir}
	}
	if !idx.Usable(embedder) {
		return nil, fmt.Errorf("vector sidecar in %s is stale (format v%d, %s-endian, embedder %s): run `kg sync --rebuild`",
			cacheDir, idx.Version, idx.ByteOrder, shortDigest(idx.Embedder))
	}

	floats, unmap, err := mapFloats(BinPath(cacheDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, &ErrNoSidecar{Dir: cacheDir}
		}
		return nil, err
	}
	want := idx.Rows * idx.Dim
	if len(floats) != want {
		unmap()
		return nil, fmt.Errorf("vector sidecar in %s is inconsistent: index claims %d rows of %d, file holds %d floats: run `kg sync --rebuild`",
			cacheDir, idx.Rows, idx.Dim, len(floats))
	}
	return &Store{idx: idx, floats: floats, unmap: unmap, byKey: idx.byKey()}, nil
}

func shortDigest(d string) string {
	if len(d) > 14 {
		return d[:14] + "…"
	}
	if d == "" {
		return "unrecorded"
	}
	return d
}

// Close releases the mapping.
func (s *Store) Close() error {
	if s == nil || s.unmap == nil {
		return nil
	}
	err := s.unmap()
	s.unmap = nil
	s.floats = nil
	return err
}

// Dim is the vector dimension.
func (s *Store) Dim() int { return s.idx.Dim }

// Len is the number of rows.
func (s *Store) Len() int { return s.idx.Rows }

// Index exposes the sidecar's index for callers that need the entries.
func (s *Store) Index() *Index { return s.idx }

// Row returns a view of row i. The returned slice aliases the mapping and
// must not be modified or retained past Close.
func (s *Store) Row(i int) []float64 {
	if i < 0 || i >= s.idx.Rows {
		return nil
	}
	off := i * s.idx.Dim
	return s.floats[off : off+s.idx.Dim : off+s.idx.Dim]
}

// Entry returns the index entry for one (entity, field) pair in constant time.
func (s *Store) Entry(id, field string) (Entry, bool) {
	e, ok := s.byKey[[2]string{id, normalizeField(field)}]
	return e, ok
}

// Vector returns the stored vector for one (entity, field) pair.
func (s *Store) Vector(id, field string) ([]float64, bool) {
	e, ok := s.Entry(id, field)
	if !ok {
		return nil, false
	}
	return s.Row(e.Row), true
}

// normalizeField accepts either the logical field name ("description") or the
// attribute name embeddings used to be written under ("description_embedding")
// and returns the logical one. Callers written against the old attribute
// naming keep working.
func normalizeField(field string) string {
	return strings.TrimSuffix(field, "_embedding")
}

// Query describes a similarity search over the sidecar.
type Query struct {
	Vector []float64
	// Type restricts the search to one entity type. Empty searches all types.
	Type string
	// Fields restricts the search to these logical field names. Empty
	// searches all fields.
	Fields []string
	// Metric is "cosine" (default) or "dot".
	Metric string
	// TopN caps the result count. Zero or less returns every match.
	TopN int
}

// Match is one scored (entity, field) pair.
type Match struct {
	ID    string
	Type  string
	Field string
	Score float64
}

// Search scans every row the filters admit and returns the highest scoring
// matches, best first.
//
// It is a brute-force scan on purpose. At the corpus ceiling ADR-0026 states,
// the scan is memory-bandwidth bound and finishes in tens of milliseconds,
// which is faster than opening any index that would have to be kept warm —
// and unlike an approximate index it is exact, so its answer does not depend
// on how the index was built.
func (s *Store) Search(q Query) ([]Match, error) {
	if len(q.Vector) != s.idx.Dim {
		return nil, fmt.Errorf("query vector has %d dimensions, sidecar has %d", len(q.Vector), s.idx.Dim)
	}

	var fields map[string]bool
	if len(q.Fields) > 0 {
		fields = make(map[string]bool, len(q.Fields))
		for _, f := range q.Fields {
			fields[normalizeField(f)] = true
		}
	}

	// The query norm is the same for every row, so hoist it out of the loop.
	// Computing it exactly as CosineSimilarity does keeps scores bit-identical
	// to the reference implementation.
	var queryNorm float64
	for _, v := range q.Vector {
		queryNorm += v * v
	}
	queryNorm = math.Sqrt(queryNorm)

	dot := q.Metric == "dot"

	matches := make([]Match, 0, 64)
	for _, e := range s.idx.Entries {
		if q.Type != "" && e.Type != q.Type {
			continue
		}
		if fields != nil && !fields[e.Field] {
			continue
		}
		row := s.Row(e.Row)
		if len(row) != len(q.Vector) {
			continue
		}
		var score float64
		if dot {
			score = dotProduct(q.Vector, row)
		} else {
			score = cosine(q.Vector, row, queryNorm)
		}
		matches = append(matches, Match{ID: e.ID, Type: e.Type, Field: e.Field, Score: score})
	}

	// Descending by score, then by id and field, so equal scores come back in
	// the same order on every run (P6).
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Score != matches[j].Score {
			return matches[i].Score > matches[j].Score
		}
		if matches[i].ID != matches[j].ID {
			return matches[i].ID < matches[j].ID
		}
		return matches[i].Field < matches[j].Field
	})

	if q.TopN > 0 && len(matches) > q.TopN {
		matches = matches[:q.TopN]
	}
	return matches, nil
}

// cosine returns the cosine similarity of a and b given a's precomputed norm.
func cosine(a, b []float64, normA float64) float64 {
	var d, normB float64
	for i := range a {
		d += a[i] * b[i]
		normB += b[i] * b[i]
	}
	normB = math.Sqrt(normB)
	if normA == 0 || normB == 0 {
		return 0
	}
	return d / (normA * normB)
}

func dotProduct(a, b []float64) float64 {
	var sum float64
	for i := range a {
		sum += a[i] * b[i]
	}
	return sum
}
