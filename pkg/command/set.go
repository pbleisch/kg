package command

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/pbleisch/kg/pkg/schema"
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

	// Attempt to load a schema for the entity's type so we can parse values.
	var s *schema.Schema
	parts := strings.SplitN(id, ":", 2)
	if len(parts) == 2 {
		typeName := parts[0]
		s, err = schema.LoadSchema(kgDir, typeName)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("loading schema: %w", err)
		}
	}

	for k, v := range attrs {
		if s != nil {
			if def, ok := s.Attributes[k]; ok {
				pv, perr := parseAttrValue(def.Type, v)
				if perr != nil {
					return nil, fmt.Errorf("attribute %s: %w", k, perr)
				}
				e.Attributes[k] = pv
				continue
			}
		}
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
