package command

import (
	"fmt"

	"github.com/pbleisch/kg/pkg/store"
)

// Set updates attributes on an existing entity, merging new values into the
// existing attribute map. It returns the updated entity.
func Set(kgDir, id string, attrs map[string]string) (*store.Entity, error) {
	e, err := store.ReadEntity(kgDir, id)
	if err != nil {
		return nil, fmt.Errorf("reading entity: %w", err)
	}

	if e.Attributes == nil {
		e.Attributes = make(map[string]any, len(attrs))
	}
	for k, v := range attrs {
		e.Attributes[k] = v
	}

	if err := store.WriteEntity(kgDir, e); err != nil {
		return nil, fmt.Errorf("writing entity: %w", err)
	}

	if err := store.Sync(kgDir); err != nil {
		return nil, fmt.Errorf("syncing index: %w", err)
	}

	return e, nil
}
