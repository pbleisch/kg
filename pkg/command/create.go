package command

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode"

	"github.com/pbleisch/kg/pkg/schema"
	"github.com/pbleisch/kg/pkg/store"
)

// Create builds and persists a new entity, returning the created entity.
// The typeName is lowercased for the entity ID and PascalCased for the Type field.
func Create(kgDir, typeName, localID string, attrs map[string]string) (*store.Entity, error) {
	lowerType := strings.ToLower(typeName)
	entityID := lowerType + ":" + localID

	// Check for duplicates.
	if _, err := store.ReadEntity(kgDir, entityID); err == nil {
		return nil, fmt.Errorf("entity %q already exists", entityID)
	}

	e := &store.Entity{
		ID:         entityID,
		Type:       pascalCase(lowerType),
		Attributes: make(map[string]any, len(attrs)),
	}
	for k, v := range attrs {
		e.Attributes[k] = v
	}

	// Validate against schema if one exists.
	s, err := schema.LoadSchema(kgDir, lowerType)
	if err == nil {
		if errs := schema.ValidateEntity(s, e); len(errs) > 0 {
			msgs := make([]string, len(errs))
			for i, ve := range errs {
				msgs[i] = ve.Error()
			}
			return nil, fmt.Errorf("validation failed: %s", strings.Join(msgs, "; "))
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("loading schema: %w", err)
	}

	if err := store.WriteEntity(kgDir, e); err != nil {
		return nil, fmt.Errorf("writing entity: %w", err)
	}

	if err := store.Sync(kgDir); err != nil {
		return nil, fmt.Errorf("syncing index: %w", err)
	}

	return e, nil
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

// pascalCase capitalises the first letter of s.
func pascalCase(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}
