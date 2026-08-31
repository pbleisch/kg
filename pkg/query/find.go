package query

import (
	"fmt"
	"strings"

	"github.com/pbleisch/kg/pkg/schema"
	"github.com/pbleisch/kg/pkg/store"
)

// Filter describes a single attribute filter for a find query.
type Filter = store.AttrFilter

// opTokens lists the operators in the order they should be tried when parsing.
// Longer operators must come first to avoid partial matches.
var opTokens = []string{">=", "<=", "!=", "~=", ">", "<", "=", "~"}

// ParseFilter parses a filter expression like "role=Engineer" or "salary>=100000".
func ParseFilter(expr string) (Filter, error) {
	for _, op := range opTokens {
		if idx := strings.Index(expr, op); idx > 0 {
			attr := strings.TrimSpace(expr[:idx])
			value := strings.TrimSpace(expr[idx+len(op):])
			if attr == "" {
				return Filter{}, fmt.Errorf("empty attribute in filter %q", expr)
			}
			// Normalize ~= to ~
			actualOp := op
			if actualOp == "~=" {
				actualOp = "~"
			}
			return Filter{Attr: attr, Op: actualOp, Value: value}, nil
		}
	}
	return Filter{}, fmt.Errorf("no operator found in filter %q", expr)
}

// Find searches for entities of the given type that match all filters.
// The typeName is expected to be snake_case (normalized at the CLI edge).
func Find(kgDir, typeName string, filters []Filter) ([]*store.Entity, error) {
	return FindWithOpts(kgDir, typeName, store.QueryOptions{
		Filters:          filters,
		IncludeRelations: true,
	})
}

// FindWithOpts is like Find but accepts full QueryOptions for pagination
// and embedding control.
//
// It reads one directory — <kgDir>/entities/<type>/ — not the whole graph, so
// a typed find over a large corpus costs what that type costs (ADR-0026).
func FindWithOpts(kgDir, typeName string, opts store.QueryOptions) ([]*store.Entity, error) {
	results, err := store.FindByType(kgDir, typeName, opts)
	if err != nil {
		return nil, fmt.Errorf("finding type %s: %w", typeName, err)
	}

	// If no entities found, attempt case-insensitive resolution using schemas.
	if len(results) == 0 {
		if schemas, serr := schema.LoadAllSchemas(kgDir); serr == nil {
			for name := range schemas {
				if strings.EqualFold(name, typeName) && name != typeName {
					if r2, err2 := store.FindByType(kgDir, name, opts); err2 == nil && len(r2) > 0 {
						results = r2
						break
					}
				}
			}
		}
	}

	return results, nil
}
