package command

import (
	"fmt"

	"github.com/pbleisch/kg/pkg/store"
)

// Delete removes an entity by ID. The file is the graph, so removing the
// file is the whole operation.
func Delete(kgDir, id string) error {
	if _, err := store.ReadEntity(kgDir, id); err != nil {
		return fmt.Errorf("entity %q not found: %w", id, err)
	}

	if err := store.DeleteEntity(kgDir, id); err != nil {
		return fmt.Errorf("deleting entity: %w", err)
	}

	return nil
}
