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
		v := e.Attributes[k]
		switch {
		case !isList(v):
			fmt.Fprintf(w, "%s: %v\n", k, v)
		case anyAttributed(v):
			// Attributed items keep their sources, in the tabular shape this
			// format already uses for a set of records.  Rendering them with
			// %v would emit `[map[source:… text:…]]`, which is Go's debug
			// formatting and not a value any consumer can read (P6).
			items := store.DecodeItems(v)
			fmt.Fprintf(w, "%s[%d]{text,source}:\n", k, len(items))
			for _, it := range items {
				fmt.Fprintf(w, "  %s,%s\n", quoteField(it.TextString()), quoteField(it.Source))
			}
		default:
			fields := itemFields(v)
			fmt.Fprintf(w, "%s[%d]: %s\n", k, len(fields), strings.Join(fields, ","))
		}
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
					// One cell, one field: a list's items are joined with the
					// same ";" relations already use inside a cell, and each
					// item is quoted if it carries the row separator.
					if isList(v) {
						vals = append(vals, quoteField(strings.Join(store.AttrTexts(v), ";")))
					} else {
						vals = append(vals, fmt.Sprintf("%v", v))
					}
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

func (f *toonFormatter) FormatSchema(w io.Writer, s SchemaInfo) error {
	fmt.Fprintf(w, "type: %s\n", s.Type)
	if s.Description != "" {
		fmt.Fprintf(w, "description: %s\n", s.Description)
	}
	if len(s.Attributes) > 0 {
		parts := make([]string, len(s.Attributes))
		for i, a := range s.Attributes {
			req := ""
			if a.Required {
				req = "(required)"
			}
			parts[i] = a.Name + ":" + a.Type + req
		}
		fmt.Fprintf(w, "attributes[%d]: %s\n", len(s.Attributes), strings.Join(parts, ","))
	}
	if len(s.Relations) > 0 {
		parts := make([]string, len(s.Relations))
		for i, r := range s.Relations {
			parts[i] = fmt.Sprintf("%s->%s[%s]", r.Name, r.Target, r.Cardinality)
		}
		fmt.Fprintf(w, "relations[%d]: %s\n", len(s.Relations), strings.Join(parts, ","))
	}
	return nil
}

func (f *toonFormatter) FormatTypeList(w io.Writer, names []string) error {
	fmt.Fprintf(w, "types[%d]: %s\n", len(names), strings.Join(names, ","))
	return nil
}

func (f *toonFormatter) FormatPathIDs(w io.Writer, ids []string) error {
	for _, id := range ids {
		fmt.Fprintln(w, id)
	}
	return nil
}

func (f *toonFormatter) FormatPathVerbose(w io.Writer, results []PathResult) error {
	fmt.Fprintf(w, "path[%d]{id,depth,via}:\n", len(results))
	for _, r := range results {
		fmt.Fprintf(w, "  %s,%d,%s\n", r.ID, r.Depth, r.Via)
	}
	fmt.Fprintf(w, "count: %d\n", len(results))
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
