package command

import (
	"fmt"
	"os"
	"sort"

	"github.com/pbleisch/kg/pkg/store"
)

// DedupOptions configures deduplication behavior.
type DedupOptions struct {
	Threshold    float64  // similarity threshold (0.0–1.0)
	Fields       []string // embedding fields to compare
	DryRun       bool     // report without merging
	PreferSource bool     // source attributes win on conflict
	CrossType    bool     // compare across entity types
}

// DedupCluster is a group of similar entities to be merged.
type DedupCluster struct {
	CanonicalID string          `json:"canonical_id"`
	Duplicates  []DedupMatch    `json:"duplicates"`
}

// DedupMatch is a single duplicate entity with its similarity score.
type DedupMatch struct {
	ID    string  `json:"id"`
	Score float64 `json:"score"`
	Field string  `json:"field"`
}

// DedupResult holds the outcome of a dedup operation.
type DedupResult struct {
	Clusters        []DedupCluster `json:"clusters"`
	TotalEntities   int            `json:"total_entities"`
	DuplicatesFound int            `json:"duplicates_found"`
	ClustersFound   int            `json:"clusters_found"`
	Merged          int            `json:"merged,omitempty"`
	Warnings        []string       `json:"warnings,omitempty"`
}

// entityVectors holds precomputed vectors for one entity across all fields.
type entityVectors struct {
	entity  *store.Entity
	vectors map[string][]float64 // field → vector
}

// FindDuplicates scans entities and returns clusters of similar entities.
func FindDuplicates(kgDir, typeName string, opts DedupOptions) (*DedupResult, error) {
	var entities []*store.Entity
	var err error

	if opts.CrossType || typeName == "" {
		entities, err = store.ListEntities(kgDir)
	} else {
		entities, err = store.ListEntitiesByType(kgDir, typeName)
	}
	if err != nil {
		// No entities directory or type directory — return empty result.
		if os.IsNotExist(err) {
			return &DedupResult{}, nil
		}
		return nil, fmt.Errorf("listing entities: %w", err)
	}

	result := &DedupResult{TotalEntities: len(entities)}

	// Extract vectors for all entities.
	var indexed []entityVectors
	for _, e := range entities {
		ev := entityVectors{entity: e, vectors: make(map[string][]float64)}
		hasVector := false
		for _, field := range opts.Fields {
			embKey := field + "_embedding"
			val, ok := e.Attributes[embKey]
			if !ok {
				continue
			}
			vec, ok := store.ExtractVector(val)
			if !ok || len(vec) == 0 {
				continue
			}
			ev.vectors[field] = vec
			hasVector = true
		}
		if !hasVector {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("%s: no embeddings for fields %v", e.ID, opts.Fields))
			continue
		}
		indexed = append(indexed, ev)
	}

	if len(indexed) < 2 {
		return result, nil
	}

	// Pairwise similarity — find pairs above threshold.
	type simPair struct {
		i, j  int
		score float64
		field string
	}
	var pairs []simPair

	for i := 0; i < len(indexed); i++ {
		for j := i + 1; j < len(indexed); j++ {
			bestScore := 0.0
			bestField := ""
			for _, field := range opts.Fields {
				va, okA := indexed[i].vectors[field]
				vb, okB := indexed[j].vectors[field]
				if !okA || !okB {
					continue
				}
				score, err := store.CosineSimilarity(va, vb)
				if err != nil {
					continue
				}
				if score > bestScore {
					bestScore = score
					bestField = field
				}
			}
			if bestScore >= opts.Threshold {
				pairs = append(pairs, simPair{i, j, bestScore, bestField})
			}
		}
	}

	if len(pairs) == 0 {
		return result, nil
	}

	// Union-find to build transitive clusters.
	parent := make([]int, len(indexed))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(x int) int {
		if parent[x] != x {
			parent[x] = find(parent[x])
		}
		return parent[x]
	}
	union := func(a, b int) {
		ra, rb := find(a), find(b)
		if ra != rb {
			// Pick the one with the lexicographically lower ID as root.
			if indexed[ra].entity.ID > indexed[rb].entity.ID {
				ra, rb = rb, ra
			}
			parent[rb] = ra
		}
	}

	// Track best score per pair for reporting.
	type pairKey struct{ i, j int }
	bestPairs := make(map[pairKey]simPair)
	for _, p := range pairs {
		union(p.i, p.j)
		key := pairKey{p.i, p.j}
		if existing, ok := bestPairs[key]; !ok || p.score > existing.score {
			bestPairs[key] = p
		}
	}

	// Collect clusters.
	clusterMap := make(map[int][]int) // root → members
	for i := range indexed {
		root := find(i)
		clusterMap[root] = append(clusterMap[root], i)
	}

	for root, members := range clusterMap {
		if len(members) < 2 {
			continue
		}
		canonical := indexed[root].entity.ID
		cluster := DedupCluster{CanonicalID: canonical}
		for _, m := range members {
			if m == root {
				continue
			}
			// Find the score between this member and the canonical.
			score, field := bestMatchScore(indexed, root, m, opts.Fields, opts.Threshold)
			cluster.Duplicates = append(cluster.Duplicates, DedupMatch{
				ID:    indexed[m].entity.ID,
				Score: score,
				Field: field,
			})
			result.DuplicatesFound++
		}
		// Sort duplicates by score descending.
		sort.Slice(cluster.Duplicates, func(i, j int) bool {
			return cluster.Duplicates[i].Score > cluster.Duplicates[j].Score
		})
		result.Clusters = append(result.Clusters, cluster)
	}

	// Sort clusters by canonical ID for stable output.
	sort.Slice(result.Clusters, func(i, j int) bool {
		return result.Clusters[i].CanonicalID < result.Clusters[j].CanonicalID
	})
	result.ClustersFound = len(result.Clusters)

	return result, nil
}

// bestMatchScore finds the best similarity score between two indexed entities.
func bestMatchScore(indexed []entityVectors, i, j int, fields []string, threshold float64) (float64, string) {
	bestScore := 0.0
	bestField := ""
	for _, field := range fields {
		va, okA := indexed[i].vectors[field]
		vb, okB := indexed[j].vectors[field]
		if !okA || !okB {
			continue
		}
		score, err := store.CosineSimilarity(va, vb)
		if err != nil {
			continue
		}
		if score > bestScore {
			bestScore = score
			bestField = field
		}
	}
	return bestScore, bestField
}

// ApplyDedup merges all duplicates in the result into their canonical entities.
func ApplyDedup(kgDir string, result *DedupResult, opts DedupOptions) error {
	for _, cluster := range result.Clusters {
		for _, dup := range cluster.Duplicates {
			mergeOpts := MergeOptions{
				PreferSource: opts.PreferSource,
				CrossType:    opts.CrossType,
			}
			_, err := Merge(kgDir, dup.ID, cluster.CanonicalID, mergeOpts)
			if err != nil {
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("merge %s → %s: %v", dup.ID, cluster.CanonicalID, err))
				continue
			}
			result.Merged++
		}
	}
	return nil
}
