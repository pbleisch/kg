package output

import (
	"strings"

	"github.com/pbleisch/kg/pkg/store"
)

// isList reports whether a stored attribute value is multi-valued *as
// written*.  The printer asks the value, not the schema: it renders what the
// file holds, and a graph with no schema still has to print.
func isList(v any) bool {
	switch v.(type) {
	case []any, []string:
		return true
	}
	return false
}

// itemFields renders a multi-valued attribute's items for a one-line form:
// each item's text, quoted when it needs it.
func itemFields(v any) []string {
	items := store.DecodeItems(v)
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, quoteField(it.TextString()))
	}
	return out
}

// anyAttributed reports whether any item names a source, which decides whether
// the one-line form can carry the value without losing provenance.
func anyAttributed(v any) bool {
	for _, it := range store.DecodeItems(v) {
		if it.Source != "" {
			return true
		}
	}
	return false
}

// quoteField makes one field safe to sit in a comma-separated row.
//
// The rule is CSV's, so a consumer needs a parser rather than a regex (P6).
// It is applied to values the printer knows are fields of a list; a scalar
// string that happens to contain a comma is a pre-existing hazard of the
// tabular writer and is not changed here.
func quoteField(s string) string {
	if !strings.ContainsAny(s, ",\"\n") {
		return s
	}
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}
