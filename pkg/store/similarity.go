package store

import (
	"fmt"
	"math"
	"sort"

	"github.com/pbleisch/kg/pkg/vector"
)

// SimilarityParams controls a vector similarity query.
type SimilarityParams struct {
	TypeName string
	Vector   []float64 // query vector
	Fields   []string  // logical field names to search
	Metric   string    // "cosine" (default) or "dot"
	TopN     int
}

// SimilarityResult is one ranked result from a vector similarity query.
type SimilarityResult struct {
	Entity     *Entity
	Score      float64
	MatchField string // field that produced the best score
}

// Similar ranks entities against a query vector by scanning the sidecar.
//
// The scan is exact and brute-force. The index it replaces was an approximate
// HNSW structure inside a 1.3 GB database that had to be built before the
// first query could be answered; the sidecar is 83 MB of floats that mmap in
// and scan in tens of milliseconds. See ADR-0026 for the measurements and the
// corpus ceiling they hold for.
//
// Only the top-N entities are read from disk, so the cost of the query does
// not depend on how large the matching entities are.
func Similar(kgDir string, params SimilarityParams) ([]SimilarityResult, error) {
	// The embedder is not required to search: a caller supplying a raw vector
	// is entitled to search whatever the sidecar holds. When one is
	// configured, its digest is passed so a sidecar from a different embedder
	// is refused rather than silently compared against.
	digest := ""
	if emb, err := ResolveEmbedder(kgDir); err == nil {
		digest = emb.Digest()
	}

	vs, err := vector.Open(CacheDir(kgDir), digest)
	if err != nil {
		return nil, err
	}
	defer vs.Close()

	matches, err := vs.Search(vector.Query{
		Vector: params.Vector,
		Type:   params.TypeName,
		Fields: params.Fields,
		Metric: params.Metric,
		TopN:   params.TopN,
	})
	if err != nil {
		return nil, err
	}

	results := make([]SimilarityResult, 0, len(matches))
	for _, m := range matches {
		e, err := ReadEntity(kgDir, m.ID)
		if err != nil {
			// The sidecar names an entity the graph no longer has. That is
			// stale derived data, not a broken graph: report the id so a
			// caller can see what to rebuild, and keep going.
			e = &Entity{ID: m.ID, Type: m.Type}
		}
		results = append(results, SimilarityResult{
			Entity:     e,
			Score:      m.Score,
			MatchField: m.Field,
		})
	}
	return results, nil
}

// VectorFor returns the stored vector for one entity field, which is how
// `kg similar --like` turns an entity into a query.
func VectorFor(kgDir, entityID string, fields []string) ([]float64, string, error) {
	digest := ""
	if emb, err := ResolveEmbedder(kgDir); err == nil {
		digest = emb.Digest()
	}
	vs, err := vector.Open(CacheDir(kgDir), digest)
	if err != nil {
		return nil, "", err
	}
	defer vs.Close()

	for _, field := range fields {
		if v, ok := vs.Vector(entityID, field); ok {
			// The mapping goes away with vs, so hand back a copy.
			out := make([]float64, len(v))
			copy(out, v)
			return out, field, nil
		}
	}
	return nil, "", fmt.Errorf("entity %s has no stored vector for fields %v; run `kg sync` to embed it", entityID, fields)
}

// ExtractVector converts an attribute value to a []float64.
// Handles []float64 (native) and []any (from JSON or YAML unmarshal).
//
// Kept for corpora written before vectors moved to the sidecar, and for
// callers that hand kg a vector as data rather than reading one back.
func ExtractVector(v any) ([]float64, bool) {
	switch tv := v.(type) {
	case []float64:
		return tv, true
	case []any:
		vec := make([]float64, len(tv))
		for i, elem := range tv {
			switch fv := elem.(type) {
			case float64:
				vec[i] = fv
			case int:
				vec[i] = float64(fv)
			case int64:
				vec[i] = float64(fv)
			default:
				return nil, false
			}
		}
		return vec, true
	}
	return nil, false
}

// CosineSimilarity returns the cosine similarity of a and b.
// Returns 0 if either vector has zero magnitude.
func CosineSimilarity(a, b []float64) (float64, error) {
	if len(a) != len(b) {
		return 0, fmt.Errorf("dimension mismatch: %d vs %d", len(a), len(b))
	}
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	normA = math.Sqrt(normA)
	normB = math.Sqrt(normB)
	if normA == 0 || normB == 0 {
		return 0, nil
	}
	return dot / (normA * normB), nil
}

// DotProduct returns the dot product of a and b.
func DotProduct(a, b []float64) (float64, error) {
	if len(a) != len(b) {
		return 0, fmt.Errorf("dimension mismatch: %d vs %d", len(a), len(b))
	}
	var sum float64
	for i := range a {
		sum += a[i] * b[i]
	}
	return sum, nil
}

// computeScore applies the named metric (cosine by default) to a and b.
func computeScore(metric string, a, b []float64) (float64, error) {
	if metric == "dot" {
		return DotProduct(a, b)
	}
	return CosineSimilarity(a, b)
}

// SortResults sorts results descending by score, then by entity ID so equal
// scores come back in the same order on every run (P6).
func SortResults(results []SimilarityResult) {
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return results[i].Entity.ID < results[j].Entity.ID
	})
}

// ResolveVector extracts a vector from entity attributes for the given field.
// Resolution order: field as-is → field+"_embedding".
//
// Only legacy corpora carry these attributes; new vectors are in the sidecar.
func ResolveVector(attrs map[string]any, field string) (string, []float64) {
	if val, ok := attrs[field]; ok {
		if vec, ok := ExtractVector(val); ok && len(vec) > 0 {
			return field, vec
		}
	}
	embField := field + "_embedding"
	if val, ok := attrs[embField]; ok {
		if vec, ok := ExtractVector(val); ok && len(vec) > 0 {
			return embField, vec
		}
	}
	return "", nil
}
