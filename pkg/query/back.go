package query

import (
	"fmt"

	"github.com/pbleisch/kg/pkg/store"
)

// Back finds entities that point to targetID via the given relation.
//
// A reverse lookup is the one read that genuinely needs every entity: an
// inbound edge is recorded only on the entity that has it, so nothing short of
// reading them all can find the edges pointing here. That whole-graph walk is
// the traversal half of P4's budget (ADR-0026).
func Back(kgDir string, targetID string, relation string) ([]*store.Entity, error) {
	g, err := store.LoadGraph(kgDir)
	if err != nil {
		return nil, fmt.Errorf("loading graph: %w", err)
	}

	var entities []*store.Entity
	for _, id := range g.ReferrersVia(relation, targetID) {
		e, ok := g.Entity(id)
		if !ok {
			return nil, fmt.Errorf("get entity %s", id)
		}
		entities = append(entities, e)
	}
	return entities, nil
}

// BackRecursive performs a BFS following the reverse relation from targetID
// until no new entity IDs are discovered. It is cycle-safe.
func BackRecursive(kgDir string, targetID string, relation string) ([]string, error) {
	g, err := store.LoadGraph(kgDir)
	if err != nil {
		return nil, fmt.Errorf("loading graph: %w", err)
	}

	seen := map[string]bool{targetID: true}
	queue := []string{targetID}
	var result []string

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, id := range g.ReferrersVia(relation, current) {
			if !seen[id] {
				seen[id] = true
				result = append(result, id)
				queue = append(queue, id)
			}
		}
	}
	return result, nil
}
