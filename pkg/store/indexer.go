package store

import (
	"fmt"
	"strconv"
	"strings"
)

// Indexer defines the interface for a knowledge graph index backend.
// Implementations must support CRUD operations on entities and reverse
// lookups. The index is a disposable cache rebuilt from YAML source files.
type Indexer interface {
	// Close releases resources held by the index.
	Close() error

	// AddEntity indexes all quads (type, attributes, relations) for an entity.
	AddEntity(e *Entity) error

	// RemoveEntity removes all indexed data for the given entity ID.
	RemoveEntity(id string) error

	// UpsertEntity removes old indexed data and adds new data for an entity
	// in a single atomic operation, avoiding two separate transactions.
	UpsertEntity(e *Entity) error

	// GetEntity reconstructs an entity from indexed data.
	GetEntity(id string) (*Entity, error)

	// FindByType returns IDs of all entities with the given type.
	FindByType(typeName string) ([]string, error)

	// FindEntities returns entities of the given type matching all filters.
	// Filters are evaluated server-side to avoid N+1 round-trips over RPC.
	FindEntities(typeName string, filters []AttrFilter) ([]*Entity, error)

	// FindByRelationTarget returns IDs of entities that point to targetID
	// via the named relation.
	FindByRelationTarget(relation, targetID string) ([]string, error)

	// FindReferrers returns IDs of entities that point to the given entity
	// via any relation. Used by merge to efficiently find incoming links
	// without scanning all entities on disk.
	FindReferrers(id string) ([]string, error)
}

// BatchIndexer is an optional interface for backends that support batched
// writes in a single transaction, reducing per-entity transaction overhead.
type BatchIndexer interface {
	Indexer
	UpsertEntities(entities []*Entity) error
	RemoveEntities(ids []string) error
}

// AttrFilter describes a single attribute filter for entity queries.
type AttrFilter struct {
	Attr  string `json:"attr"`
	Op    string `json:"op"`
	Value string `json:"value"`
}

// MatchEntity returns true if the entity satisfies all filters.
func MatchFilters(e *Entity, filters []AttrFilter) bool {
	for _, f := range filters {
		if !matchFilter(e, f) {
			return false
		}
	}
	return true
}

func matchFilter(e *Entity, f AttrFilter) bool {
	raw, ok := e.Attributes[f.Attr]
	if !ok {
		return false
	}
	attrStr := fmt.Sprintf("%v", raw)
	switch f.Op {
	case "=":
		return attrStr == f.Value
	case "!=":
		return attrStr != f.Value
	case "~":
		return strings.Contains(strings.ToLower(attrStr), strings.ToLower(f.Value))
	case ">", "<", ">=", "<=":
		a, errA := strconv.ParseFloat(attrStr, 64)
		b, errB := strconv.ParseFloat(f.Value, 64)
		if errA != nil || errB != nil {
			return false
		}
		switch f.Op {
		case ">":
			return a > b
		case "<":
			return a < b
		case ">=":
			return a >= b
		case "<=":
			return a <= b
		}
	}
	return false
}

// DefaultFindEntities provides the standard FindEntities implementation
// using FindByType + GetEntity + filter. Index backends that don't override
// FindEntities can delegate to this.
func DefaultFindEntities(idx Indexer, typeName string, filters []AttrFilter) ([]*Entity, error) {
	ids, err := idx.FindByType(typeName)
	if err != nil {
		return nil, err
	}
	var results []*Entity
	for _, id := range ids {
		e, err := idx.GetEntity(id)
		if err != nil {
			return nil, fmt.Errorf("getting entity %s: %w", id, err)
		}
		if MatchFilters(e, filters) {
			results = append(results, e)
		}
	}
	return results, nil
}
