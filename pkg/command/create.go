package command

import (
	"fmt"
	"strings"

	"github.com/pbleisch/kg/pkg/store"
)

// UpsertEntity creates a new entity or merges into an existing one using
// full entity data. Attributes from e overwrite existing; relations are
// merged additively (new targets appended, existing preserved).
func UpsertEntity(kgDir string, e *store.Entity) (*store.Entity, error) {
	// Normalize IDs and type before persisting.
	e.ID = store.NormalizeEntityID(e.ID)
	e.Type = store.ToSnakeCase(e.Type)
	for rel, targets := range e.Relations {
		normRel := store.NormalizeRelation(rel)
		for i, t := range targets {
			targets[i] = store.NormalizeEntityID(t)
		}
		if normRel != rel {
			delete(e.Relations, rel)
			e.Relations[normRel] = targets
		}
	}

	existing, readErr := store.ReadEntity(kgDir, e.ID)
	if readErr != nil {
		if err := store.WriteEntity(kgDir, e); err != nil {
			return nil, fmt.Errorf("writing entity: %w", err)
		}
		return e, nil
	}

	// Merge attributes (new wins).
	if existing.Attributes == nil {
		existing.Attributes = make(map[string]any)
	}
	for k, v := range e.Attributes {
		existing.Attributes[k] = v
	}

	// Merge relations additively.
	if existing.Relations == nil {
		existing.Relations = make(map[string][]string)
	}
	for rel, targets := range e.Relations {
		for _, t := range targets {
			if !containsString(existing.Relations[rel], t) {
				existing.Relations[rel] = append(existing.Relations[rel], t)
			}
		}
	}

	if err := store.WriteEntity(kgDir, existing); err != nil {
		return nil, fmt.Errorf("writing entity: %w", err)
	}
	return existing, nil
}

func containsString(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

// ParseAttrs converts ["key=value", ...] into a map. Each argument is split
// on the first '=' sign.
func ParseAttrs(args []string) (map[string]string, error) {
	m := make(map[string]string, len(args))
	for _, a := range args {
		k, v, ok := strings.Cut(a, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("invalid attribute %q: expected key=value", a)
		}
		m[k] = v
	}
	return m, nil
}
