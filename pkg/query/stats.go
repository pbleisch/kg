package query

import (
	"fmt"
	"sort"

	"github.com/pbleisch/kg/pkg/store"
)

// GraphStats holds computed metrics about the knowledge graph.
type GraphStats = store.GraphStats

// Stats computes graph-level metrics by walking every entity file.
func Stats(kgDir string) (*GraphStats, error) {
	g, err := store.LoadGraph(kgDir)
	if err != nil {
		return nil, fmt.Errorf("loading graph: %w", err)
	}
	return g.Stats(), nil
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

// sortedKV orders by count descending, then by key, so equal counts come back
// in the same order on every run (P6).
func sortedKV(m map[string]int) []KV {
	kvs := make([]KV, 0, len(m))
	for k, v := range m {
		kvs = append(kvs, KV{k, v})
	}
	sort.Slice(kvs, func(i, j int) bool {
		if kvs[i].Count != kvs[j].Count {
			return kvs[i].Count > kvs[j].Count
		}
		return kvs[i].Key < kvs[j].Key
	})
	return kvs
}
