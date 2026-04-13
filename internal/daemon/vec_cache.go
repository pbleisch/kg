package daemon

import (
	"math"
	"strings"
	"time"

	"github.com/pbleisch/kg/pkg/store"
)

// vecEntry is one cached embedding vector, stored as float32 to halve memory.
type vecEntry struct {
	entityID   string
	entityType string
	field      string
	vec        []float32
}

// loadVecCache bulk-loads all embedding vectors from the index into memory.
// It is safe to call when the index has no embeddings (returns nil).
func (d *Daemon) loadVecCache() {
	el, ok := d.service.idx.(store.EmbeddingLister)
	if !ok {
		return
	}
	start := time.Now()
	entries, err := el.ListEmbeddings()
	if err != nil {
		d.log.Printf("vec cache: load error: %v", err)
		return
	}
	if len(entries) == 0 {
		d.log.Printf("vec cache: no embeddings found in index")
		return
	}

	cache := make(map[string]vecEntry, len(entries))
	for _, e := range entries {
		f32 := toFloat32(e.Vec)
		cache[e.EntityID+":"+e.Field] = vecEntry{
			entityID:   e.EntityID,
			entityType: e.EntityType,
			field:      e.Field,
			vec:        f32,
		}
	}

	d.vecCacheMu.Lock()
	d.vecCache = cache
	d.vecCacheMu.Unlock()
	d.log.Printf("vec cache: loaded %d vectors (%s)", len(cache), time.Since(start).Round(time.Millisecond))
}

// similarFromCache performs a brute-force cosine scan over the in-memory
// vector cache. Returns (results, true) on hit, or (nil, false) when the
// cache is empty and the caller should fall through to the index.
func (d *Daemon) similarFromCache(params store.SimilarityParams) ([]store.SimilarityResult, bool) {
	d.vecCacheMu.RLock()
	defer d.vecCacheMu.RUnlock()

	if len(d.vecCache) == 0 {
		return nil, false
	}

	// Build field filter set (accept both "name" and "name_embedding").
	fieldSet := make(map[string]bool, len(params.Fields)*2)
	for _, f := range params.Fields {
		fieldSet[f] = true
		fieldSet[f+"_embedding"] = true
	}

	queryVec := toFloat32(params.Vector)

	type scored struct {
		score float64
		field string
	}
	best := make(map[string]scored) // entityID → best match

	for _, entry := range d.vecCache {
		if entry.entityType != params.TypeName {
			continue
		}
		if !fieldSet[entry.field] {
			continue
		}

		var score float64
		if params.Metric == "dot" {
			score = dotProductF32(queryVec, entry.vec)
		} else {
			score = cosineSimilarityF32(queryVec, entry.vec)
		}

		if prev, ok := best[entry.entityID]; !ok || score > prev.score {
			best[entry.entityID] = scored{score: score, field: entry.field}
		}
	}

	results := make([]store.SimilarityResult, 0, len(best))
	for id, s := range best {
		results = append(results, store.SimilarityResult{
			Entity:     &store.Entity{ID: id, Type: params.TypeName},
			Score:      s.score,
			MatchField: s.field,
		})
	}
	store.SortResults(results)
	if params.TopN > 0 && len(results) > params.TopN {
		results = results[:params.TopN]
	}
	return results, true
}

// updateVecCacheUpsert extracts embedding attributes from entities and
// inserts/replaces their entries in the vector cache.
func (d *Daemon) updateVecCacheUpsert(entities []*store.Entity) {
	d.vecCacheMu.Lock()
	defer d.vecCacheMu.Unlock()
	if d.vecCache == nil {
		return
	}
	for _, e := range entities {
		// Remove old entries for this entity.
		prefix := e.ID + ":"
		for key := range d.vecCache {
			if strings.HasPrefix(key, prefix) {
				delete(d.vecCache, key)
			}
		}
		// Add new entries from embedding attributes.
		for k, v := range e.Attributes {
			if !strings.HasSuffix(k, "_embedding") {
				continue
			}
			vec, ok := store.ExtractVector(v)
			if !ok || len(vec) == 0 {
				continue
			}
			d.vecCache[e.ID+":"+k] = vecEntry{
				entityID:   e.ID,
				entityType: e.Type,
				field:      k,
				vec:        toFloat32(vec),
			}
		}
	}
}

// updateVecCacheRemove deletes all vector cache entries for the given IDs.
func (d *Daemon) updateVecCacheRemove(ids []string) {
	d.vecCacheMu.Lock()
	defer d.vecCacheMu.Unlock()
	if d.vecCache == nil {
		return
	}
	for _, id := range ids {
		prefix := id + ":"
		for key := range d.vecCache {
			if strings.HasPrefix(key, prefix) {
				delete(d.vecCache, key)
			}
		}
	}
}

// ── float32 math helpers ────────────────────────────────────────────────

func toFloat32(v []float64) []float32 {
	out := make([]float32, len(v))
	for i, f := range v {
		out[i] = float32(f)
	}
	return out
}

func cosineSimilarityF32(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float32
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return float64(dot) / (math.Sqrt(float64(normA)) * math.Sqrt(float64(normB)))
}

func dotProductF32(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}
	var sum float32
	for i := range a {
		sum += a[i] * b[i]
	}
	return float64(sum)
}
