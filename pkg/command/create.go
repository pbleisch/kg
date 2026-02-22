package command

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode"

	"github.com/pbleisch/kg/pkg/schema"
	"github.com/pbleisch/kg/pkg/store"
)

// parseAttrValue converts a CLI string into a typed Go value based on the
// schema attribute type. Returns the parsed value or an error if parsing fails.
func parseAttrValue(attrType, raw string) (any, error) {
	switch attrType {
	case "string":
		return raw, nil
	case "number":
		// Try integer first, then float.
		if i, err := strconv.ParseInt(raw, 10, 64); err == nil {
			return i, nil
		}
		if f, err := strconv.ParseFloat(raw, 64); err == nil {
			return f, nil
		}
		return nil, fmt.Errorf("cannot parse %q as number", raw)
	case "boolean":
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("cannot parse %q as boolean", raw)
		}
		return b, nil
	case "date", "datetime":
		// Leave dates as strings for now.
		return raw, nil
	default:
		// Unknown type: return raw string and let validation handle it.
		return raw, nil
	}
}

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

	// If we have a schema, parse attribute values according to their types.
	s, err := schema.LoadSchema(kgDir, lowerType)
	if err == nil {
		for k, v := range attrs {
			if def, ok := s.Attributes[k]; ok {
				pv, perr := parseAttrValue(def.Type, v)
				if perr != nil {
					return nil, fmt.Errorf("attribute %s: %w", k, perr)
				}
				e.Attributes[k] = pv
			} else {
				e.Attributes[k] = v
			}
		}
		// Validate now that attributes are typed.
		if errs := schema.ValidateEntity(s, e); len(errs) > 0 {
			msgs := make([]string, len(errs))
			for i, ve := range errs {
				msgs[i] = ve.Error()
			}
			return nil, fmt.Errorf("validation failed: %s", strings.Join(msgs, "; "))
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("loading schema: %w", err)
	} else {
		// No schema: store raw strings.
		for k, v := range attrs {
			e.Attributes[k] = v
		}
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
