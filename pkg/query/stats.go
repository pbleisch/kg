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

// Stats computes graph-level metrics. Uses the index when a StatsProvider
// is available, falling back to scanning YAML files.
func Stats(kgDir string) (*GraphStats, error) {
	if err := store.Sync(kgDir); err != nil {
		return nil, fmt.Errorf("syncing index: %w", err)
	}

	idx, err := store.OpenIndex(kgDir)
	if err != nil {
		return nil, fmt.Errorf("opening index: %w", err)
	}
	defer idx.Close()

	if sp, ok := idx.(store.StatsProvider); ok {
		is, err := sp.IndexStats()
		if err == nil {
			return &GraphStats{
				TotalEntities: is.TotalEntities,
				TotalLinks:    is.TotalLinks,
				TypeCounts:    is.TypeCounts,
				RelCounts:     is.RelCounts,
				Orphans:       is.Orphans,
			}, nil
		}
		// Fall through to YAML scan on error.
	}

	// Fallback: scan YAML files.
	entities, err := store.ListEntities(kgDir)
	if err != nil {
		return nil, fmt.Errorf("listing entities: %w", err)
	}
	return statsFromEntities(entities), nil
}

// statsFromEntities computes stats by iterating over loaded entities.
func statsFromEntities(entities []*store.Entity) *GraphStats {
	stats := &GraphStats{
		TypeCounts: make(map[string]int),
		RelCounts:  make(map[string]int),
	}
	linked := make(map[string]bool)
	for _, e := range entities {
		stats.TotalEntities++
		stats.TypeCounts[e.Type]++
		for rel, targets := range e.Relations {
			stats.TotalLinks += len(targets)
			stats.RelCounts[rel] += len(targets)
			if len(targets) > 0 {
				linked[e.ID] = true
			}
			for _, t := range targets {
				linked[t] = true
			}
		}
	}
	for _, e := range entities {
		if !linked[e.ID] {
			stats.Orphans++
		}
	}
	return stats
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
