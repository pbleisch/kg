package store

import (
	"fmt"
	"math"
	"sort"
)

// VectorSearcher is an optional capability some index backends expose.
// Callers should type-assert an Indexer to check availability:
//
//	if vs, ok := idx.(store.VectorSearcher); ok { ... }
type VectorSearcher interface {
	SimilarEntities(params SimilarityParams) ([]SimilarityResult, error)
}

// SimilarityParams controls a vector similarity query.
type SimilarityParams struct {
	TypeName string
	Vector   []float64 // query vector
	Fields   []string  // attribute names (or logical names) to search
	Metric   string    // "cosine" (default) or "dot"
	TopN     int
}

// SimilarityResult is one ranked result from a vector similarity query.
type SimilarityResult struct {
	Entity     *Entity
	Score      float64
	MatchField string // field that produced the best score
}

// ExtractVector converts an attribute value to a []float64.
// Handles []float64 (native) and []any (from JSON unmarshal).
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

// SortResults sorts results descending by score in place.
func SortResults(results []SimilarityResult) {
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
}

// ResolveVector extracts a vector from entity attributes for the given field.
// Resolution order: field as-is → field+"_embedding". Returns the resolved
// field name and vector, or empty/nil if no vector found.
func ResolveVector(attrs map[string]any, field string) (string, []float64) {
	// Try exact field name first (might be a raw vector attribute).
	if val, ok := attrs[field]; ok {
		if vec, ok := ExtractVector(val); ok && len(vec) > 0 {
			return field, vec
		}
	}
	// Fallback: try <field>_embedding.
	embField := field + "_embedding"
	if val, ok := attrs[embField]; ok {
		if vec, ok := ExtractVector(val); ok && len(vec) > 0 {
			return embField, vec
		}
	}
	return "", nil
}
