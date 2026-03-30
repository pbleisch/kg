package query

import (
	"fmt"
	"sort"

	"github.com/pbleisch/kg/pkg/store"
)

// GraphStats holds computed metrics about the knowledge graph.
type GraphStats struct {
	TotalEntities int            `json:"total_entities"`
	TotalLinks    int            `json:"total_links"`
	TypeCounts    map[string]int `json:"type_counts"`
	RelCounts     map[string]int `json:"relation_counts"`
	Orphans       int            `json:"orphans"`
}

// Stats computes graph-level metrics by scanning all entity YAML files.
func Stats(kgDir string) (*GraphStats, error) {
	entities, err := store.ListEntities(kgDir)
	if err != nil {
		return nil, fmt.Errorf("listing entities: %w", err)
	}

	stats := &GraphStats{
		TypeCounts: make(map[string]int),
		RelCounts:  make(map[string]int),
	}

	// Set of all entity IDs that are referenced (incoming or outgoing).
	linked := make(map[string]bool)

	for _, e := range entities {
		stats.TotalEntities++
		stats.TypeCounts[e.Type]++

		hasLinks := false
		for rel, targets := range e.Relations {
			stats.TotalLinks += len(targets)
			stats.RelCounts[rel] += len(targets)
			if len(targets) > 0 {
				hasLinks = true
				linked[e.ID] = true
			}
			for _, t := range targets {
				linked[t] = true
			}
		}
		if !hasLinks && !linked[e.ID] {
			// Mark as potentially orphaned (will be rechecked below).
		}
	}

	// Orphans: entities with no outgoing links AND no incoming links.
	for _, e := range entities {
		if !linked[e.ID] {
			stats.Orphans++
		}
	}

	return stats, nil
}

// TypeCountsSorted returns type counts sorted by count descending.
func TypeCountsSorted(m map[string]int) []KV {
	return sortedKV(m)
}

// RelCountsSorted returns relation counts sorted by count descending.
func RelCountsSorted(m map[string]int) []KV {
	return sortedKV(m)
}

// KV is a key-value pair for sorted output.
type KV struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

func sortedKV(m map[string]int) []KV {
	kvs := make([]KV, 0, len(m))
	for k, v := range m {
		kvs = append(kvs, KV{k, v})
	}
	sort.Slice(kvs, func(i, j int) bool {
		return kvs[i].Count > kvs[j].Count
	})
	return kvs
}
