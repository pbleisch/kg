package query

import (
	"fmt"

	"github.com/pbleisch/kg/pkg/store"
)

// Get retrieves a single entity by ID from the knowledge graph.
func Get(kgDir, id string) (*store.Entity, error) {
	if err := store.Sync(kgDir); err != nil {
		return nil, fmt.Errorf("syncing index: %w", err)
	}

	idx, err := store.OpenIndex(kgDir)
	if err != nil {
		return nil, fmt.Errorf("opening index: %w", err)
	}
	defer idx.Close()

	e, err := idx.GetEntity(id)
	if err != nil {
		return nil, &QueryError{
			Code:    "entity_not_found",
			Message: fmt.Sprintf("entity %q not found", id),
		}
	}
	return e, nil
}
