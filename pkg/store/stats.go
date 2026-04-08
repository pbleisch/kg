package store

import (
	"strings"

	bolt "go.etcd.io/bbolt"
)

// IndexStats holds graph-level metrics computed from the index.
type IndexStats struct {
	TotalEntities int            `json:"total_entities"`
	TotalLinks    int            `json:"total_links"`
	TypeCounts    map[string]int `json:"type_counts"`
	RelCounts     map[string]int `json:"relation_counts"`
	Orphans       int            `json:"orphans"`
}

// StatsProvider is an optional interface for index backends that can compute
// graph statistics directly from the index without parsing YAML files.
type StatsProvider interface {
	IndexStats() (*IndexStats, error)
}

// IndexStats computes graph statistics from a single sequential scan of the
// SPO bucket. O(N) in quads, zero YAML parsing.
func (idx *boltIndex) IndexStats() (*IndexStats, error) {
	stats := &IndexStats{
		TypeCounts: make(map[string]int),
		RelCounts:  make(map[string]int),
	}

	// Track which entity IDs participate in at least one relation.
	linked := make(map[string]bool)
	allIDs := make(map[string]bool)

	err := idx.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketSPO)
		if b == nil {
			return nil
		}
		c := b.Cursor()
		for k, _ := c.First(); k != nil; k, _ = c.Next() {
			key := string(k)
			parts := strings.SplitN(key, sep, 3)
			if len(parts) != 3 {
				continue
			}
			subj, pred, obj := parts[0], parts[1], parts[2]

			switch {
			case pred == "type":
				stats.TotalEntities++
				stats.TypeCounts[obj]++
				allIDs[subj] = true
			case strings.HasPrefix(pred, "rel:"):
				relName := pred[4:]
				stats.TotalLinks++
				stats.RelCounts[relName]++
				linked[subj] = true
				linked[obj] = true
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	for id := range allIDs {
		if !linked[id] {
			stats.Orphans++
		}
	}

	return stats, nil
}
