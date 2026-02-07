package output

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/pbleisch/kg/pkg/store"
)

type toonFormatter struct{}

func (f *toonFormatter) FormatEntity(w io.Writer, e *store.Entity) error {
	fmt.Fprintf(w, "id: %s\n", e.ID)
	fmt.Fprintf(w, "type: %s\n", e.Type)

	// Collect and sort attribute keys.
	attrKeys := make([]string, 0, len(e.Attributes))
	for k := range e.Attributes {
		attrKeys = append(attrKeys, k)
	}
	sort.Strings(attrKeys)

	for _, k := range attrKeys {
		fmt.Fprintf(w, "%s: %v\n", k, e.Attributes[k])
	}

	// Collect and sort relation keys.
	relKeys := make([]string, 0, len(e.Relations))
	for k := range e.Relations {
		relKeys = append(relKeys, k)
	}
	sort.Strings(relKeys)

	for _, k := range relKeys {
		targets := e.Relations[k]
		fmt.Fprintf(w, "%s[%d]: %s\n", k, len(targets), strings.Join(targets, ","))
	}

	return nil
}

func (f *toonFormatter) FormatEntities(w io.Writer, entities []*store.Entity) error {
	if len(entities) == 0 {
		fmt.Fprintln(w, "results[0]{}:")
		fmt.Fprintln(w, "count: 0")
		return nil
	}

	// Build column set: id, type, then sorted attribute keys, then sorted relation keys.
	attrSet := map[string]bool{}
	relSet := map[string]bool{}
	for _, e := range entities {
		for k := range e.Attributes {
			attrSet[k] = true
		}
		for k := range e.Relations {
			relSet[k] = true
		}
	}

	attrKeys := sortedKeys(attrSet)
	relKeys := sortedKeys(relSet)

	columns := []string{"id", "type"}
	columns = append(columns, attrKeys...)
	columns = append(columns, relKeys...)

	fmt.Fprintf(w, "results[%d]{%s}:\n", len(entities), strings.Join(columns, ","))

	for _, e := range entities {
		vals := make([]string, 0, len(columns))
		for _, col := range columns {
			switch col {
			case "id":
				vals = append(vals, e.ID)
			case "type":
				vals = append(vals, e.Type)
			default:
				if v, ok := e.Attributes[col]; ok {
					vals = append(vals, fmt.Sprintf("%v", v))
				} else if targets, ok := e.Relations[col]; ok {
					vals = append(vals, strings.Join(targets, ";"))
				} else {
					vals = append(vals, "")
				}
			}
		}
		fmt.Fprintf(w, "  %s\n", strings.Join(vals, ","))
	}

	fmt.Fprintf(w, "count: %d\n", len(entities))
	return nil
}

func (f *toonFormatter) FormatError(w io.Writer, code string, message string, suggestions []string) error {
	fmt.Fprintf(w, "error: %s\n", code)
	fmt.Fprintf(w, "message: %s\n", message)
	if len(suggestions) > 0 {
		fmt.Fprintf(w, "suggestions[%d]: %s\n", len(suggestions), strings.Join(suggestions, ","))
	}
	return nil
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
