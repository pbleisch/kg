package schema

import (
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/pbleisch/kg/pkg/store"
)

// parseAttrValue converts a producer's string into a typed Go value using the
// schema's declared attribute type.
func parseAttrValue(attrType, raw string) (any, error) {
	switch attrType {
	case "string":
		return raw, nil
	case "number":
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
		return raw, nil
	case TypeList:
		// One string, one item.  **Never split on a separator.**
		// `categories=design,mechanic` becomes a one-element list holding
		// "design,mechanic", which is exactly what that value means today;
		// splitting it would silently shred every value — every description,
		// every quoted sentence — that legitimately contains a comma.
		return []any{raw}, nil
	default:
		return raw, nil
	}
}

// CoerceAttrs types a producer's string attributes against the schema for
// typeName.  Attributes the schema does not declare pass through as strings —
// content is advised, not enforced (P3).  A type that has no schema yields the
// strings unchanged.
//
// This lives beside the schema rather than beside a caller so that every
// producer — CLI, MCP, a Go caller — types values the same way.
func CoerceAttrs(kgDir, typeName string, attrs map[string]string) (map[string]any, error) {
	out := make(map[string]any, len(attrs))
	s, err := LoadSchema(kgDir, store.ToSnakeCase(typeName))
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("loading schema: %w", err)
		}
		for k, v := range attrs {
			out[k] = v
		}
		return out, nil
	}
	for k, v := range attrs {
		if def, ok := s.Attributes[k]; ok {
			pv, perr := parseAttrValue(def.Type, v)
			if perr != nil {
				return nil, fmt.Errorf("attribute %s: %w", k, perr)
			}
			out[k] = pv
			continue
		}
		out[k] = v
	}
	return out, nil
}
