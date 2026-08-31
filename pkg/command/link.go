package command

import (
	"fmt"

	"github.com/pbleisch/kg/pkg/store"
)

// Link adds targetID to the relation list on sourceID, avoiding duplicates.
//
// A missing source is an error.  This used to mint a typeless stub entity
// instead, "common in LLM pipelines where links precede entity creation" — and
// that is precisely how a graph acquires entities nobody submitted.  Callers
// come through the batch boundary, which refuses a relation whose ends do not
// resolve before any write happens.
func Link(kgDir, sourceID, relation, targetID string) error {
	e, err := store.ReadEntity(kgDir, sourceID)
	if err != nil {
		if _, refused := store.LoadRefused(err); refused {
			return fmt.Errorf("linking from %q: %w", sourceID, err)
		}
		return fmt.Errorf("linking from %q: source does not exist", sourceID)
	}

	if e.Relations == nil {
		e.Relations = make(map[string][]string)
	}

	for _, id := range e.Relations[relation] {
		if id == targetID {
			return nil // already linked
		}
	}
	e.Relations[relation] = append(e.Relations[relation], targetID)

	if err := store.WriteEntity(kgDir, e); err != nil {
		return fmt.Errorf("writing entity: %w", err)
	}
	return nil
}

// Unlink removes targetID from the relation list on sourceID.
func Unlink(kgDir, sourceID, relation, targetID string) error {
	e, err := store.ReadEntity(kgDir, sourceID)
	if err != nil {
		return fmt.Errorf("reading source entity: %w", err)
	}

	targets, ok := e.Relations[relation]
	if !ok {
		return fmt.Errorf("relation %q not found on entity %q", relation, sourceID)
	}

	idx := -1
	for i, id := range targets {
		if id == targetID {
			idx = i
			break
		}
	}
	if idx == -1 {
		return fmt.Errorf("target %q not found in relation %q on entity %q", targetID, relation, sourceID)
	}

	targets = append(targets[:idx], targets[idx+1:]...)
	if len(targets) == 0 {
		delete(e.Relations, relation)
	} else {
		e.Relations[relation] = targets
	}

	if err := store.WriteEntity(kgDir, e); err != nil {
		return fmt.Errorf("writing entity: %w", err)
	}
	return nil
}
