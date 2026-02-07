package command

import (
	"fmt"

	"github.com/pbleisch/kg/pkg/store"
)

// Delete removes an entity by ID and re-syncs the index.
func Delete(kgDir, id string) error {
	if _, err := store.ReadEntity(kgDir, id); err != nil {
		return fmt.Errorf("entity %q not found: %w", id, err)
	}

	if err := store.DeleteEntity(kgDir, id); err != nil {
		return fmt.Errorf("deleting entity: %w", err)
	}

	if err := store.Sync(kgDir); err != nil {
		return fmt.Errorf("syncing index: %w", err)
	}

	return nil
}
