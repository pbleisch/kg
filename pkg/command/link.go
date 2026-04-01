package command

import (
	"fmt"
	"strings"

	"github.com/pbleisch/kg/pkg/store"
)

// Link adds targetID to the relation list on sourceID, avoiding duplicates.
// If the source entity doesn't exist on disk, a stub entity is created
// automatically (common in LLM pipelines where links precede entity creation).
func Link(kgDir, sourceID, relation, targetID string) error {
	e, err := store.ReadEntity(kgDir, sourceID)
	if err != nil {
		// Source doesn't exist — create a stub entity.
		parts := strings.SplitN(sourceID, ":", 2)
		typeName := ""
		if len(parts) == 2 {
			typeName = parts[0]
		}
		e = &store.Entity{
			ID:         sourceID,
			Type:       typeName,
			Attributes: map[string]any{},
			Relations:  map[string][]string{relation: {targetID}},
		}
		if err := store.WriteEntity(kgDir, e); err != nil {
			return fmt.Errorf("creating stub entity: %w", err)
		}
		if err := store.Sync(kgDir); err != nil {
			return fmt.Errorf("syncing index: %w", err)
		}
		return nil
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
	if err := store.Sync(kgDir); err != nil {
		return fmt.Errorf("syncing index: %w", err)
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
	if err := store.Sync(kgDir); err != nil {
		return fmt.Errorf("syncing index: %w", err)
	}
	return nil
}
