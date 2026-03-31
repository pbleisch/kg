package query

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/pbleisch/kg/pkg/schema"
	"github.com/pbleisch/kg/pkg/store"
)

// Filter describes a single attribute filter for a find query.
type Filter struct {
	Attr  string
	Op    string
	Value string
}

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
	if err := store.Sync(kgDir); err != nil {
		return nil, fmt.Errorf("syncing index: %w", err)
	}

	idx, err := store.OpenIndex(kgDir)
	if err != nil {
		return nil, fmt.Errorf("opening index: %w", err)
	}
	defer idx.Close()

	ids, err := idx.FindByType(typeName)
	if err != nil {
		return nil, fmt.Errorf("finding type %s: %w", typeName, err)
	}

	// If no entities found, attempt case-insensitive resolution using schemas.
	if len(ids) == 0 {
		if schemas, serr := schema.LoadAllSchemas(kgDir); serr == nil {
			for name := range schemas {
				if strings.EqualFold(name, typeName) && name != typeName {
					if ids2, err2 := idx.FindByType(name); err2 == nil && len(ids2) > 0 {
						ids = ids2
						break
					}
				}
			}
		}
	}

	var results []*store.Entity
	for _, id := range ids {
		e, err := idx.GetEntity(id)
		if err != nil {
			return nil, fmt.Errorf("getting entity %s: %w", id, err)
		}
		if matchAll(e, filters) {
			results = append(results, e)
		}
	}
	return results, nil
}

func matchAll(e *store.Entity, filters []Filter) bool {
	for _, f := range filters {
		if !matchFilter(e, f) {
			return false
		}
	}
	return true
}

func matchFilter(e *store.Entity, f Filter) bool {
	raw, ok := e.Attributes[f.Attr]
	if !ok {
		return false
	}

	attrStr := fmt.Sprintf("%v", raw)

	switch f.Op {
	case "=":
		return attrStr == f.Value
	case "!=":
		return attrStr != f.Value
	case "~":
		return strings.Contains(strings.ToLower(attrStr), strings.ToLower(f.Value))
	case ">", "<", ">=", "<=":
		return compareNumeric(attrStr, f.Op, f.Value)
	default:
		return false
	}
}

func compareNumeric(attrStr, op, filterVal string) bool {
	a, errA := strconv.ParseFloat(attrStr, 64)
	b, errB := strconv.ParseFloat(filterVal, 64)
	if errA != nil || errB != nil {
		return false
	}
	switch op {
	case ">":
		return a > b
	case "<":
		return a < b
	case ">=":
		return a >= b
	case "<=":
		return a <= b
	}
	return false
}
