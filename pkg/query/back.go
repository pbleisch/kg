package query

import (
	"fmt"

	"github.com/pbleisch/kg/pkg/store"
)

// Back finds entities that point to targetID via the given relation.
// It syncs the index, performs a reverse lookup, and returns full entities.
func Back(kgDir string, targetID string, relation string) ([]*store.Entity, error) {
	if err := store.Sync(kgDir); err != nil {
		return nil, fmt.Errorf("sync: %w", err)
	}

	idx, err := store.OpenIndex(kgDir)
	if err != nil {
		return nil, fmt.Errorf("open index: %w", err)
	}
	defer idx.Close()

	ids, err := idx.FindByRelationTarget(relation, targetID)
	if err != nil {
		return nil, fmt.Errorf("reverse lookup: %w", err)
	}

	var entities []*store.Entity
	for _, id := range ids {
		e, err := idx.GetEntity(id)
		if err != nil {
			return nil, fmt.Errorf("get entity %s: %w", id, err)
		}
		entities = append(entities, e)
	}
	return entities, nil
}

// BackRecursive performs a BFS following the reverse relation from targetID
// until no new entity IDs are discovered. It is cycle-safe.
func BackRecursive(kgDir string, targetID string, relation string) ([]string, error) {
	if err := store.Sync(kgDir); err != nil {
		return nil, fmt.Errorf("sync: %w", err)
	}

	idx, err := store.OpenIndex(kgDir)
	if err != nil {
		return nil, fmt.Errorf("open index: %w", err)
	}
	defer idx.Close()

	seen := map[string]bool{targetID: true}
	queue := []string{targetID}
	var result []string

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		ids, err := idx.FindByRelationTarget(relation, current)
		if err != nil {
			return nil, fmt.Errorf("reverse lookup %s: %w", current, err)
		}
		for _, id := range ids {
			if !seen[id] {
				seen[id] = true
				result = append(result, id)
				queue = append(queue, id)
			}
		}
	}
	return result, nil
}
