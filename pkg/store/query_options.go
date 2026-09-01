package store

import (
	"strconv"
	"strings"
)

// AttrFilter describes a single attribute filter for entity queries.
type AttrFilter struct {
	Attr  string `json:"attr"`
	Op    string `json:"op"`
	Value string `json:"value"`
}

// QueryOptions controls how a set of entities is selected and projected.
type QueryOptions struct {
	Filters           []AttrFilter `json:"filters,omitempty"`
	Offset            int          `json:"offset,omitempty"`
	Limit             int          `json:"limit,omitempty"` // 0 = no limit
	IncludeEmbeddings bool         `json:"include_embeddings,omitempty"`
	IncludeRelations  bool         `json:"include_relations,omitempty"`
}

// MatchFilters returns true if the entity satisfies all filters.
func MatchFilters(e *Entity, filters []AttrFilter) bool {
	for _, f := range filters {
		if !matchFilter(e, f) {
			return false
		}
	}
	return true
}

// matchFilter tests one filter against one entity.
//
// A multi-valued attribute matches when ANY of its items matches: `categories`
// holding two items is two answers to one question, and a filter asking for
// one of them means it.  A scalar is a one-element list, so this is the same
// rule rather than a second one.
func matchFilter(e *Entity, f AttrFilter) bool {
	raw, ok := e.Attributes[f.Attr]
	if !ok {
		return false
	}
	texts := AttrTexts(raw)
	if f.Op == "!=" {
		// "no item equals", which is the negation of "=".  Read as "some item
		// differs", a two-item list would satisfy != against its own values.
		for _, s := range texts {
			if s == f.Value {
				return false
			}
		}
		return true
	}
	for _, s := range texts {
		if matchText(s, f) {
			return true
		}
	}
	return false
}

func matchText(attrStr string, f AttrFilter) bool {
	switch f.Op {
	case "=":
		return attrStr == f.Value
	case "!=":
		return attrStr != f.Value
	case "~":
		return strings.Contains(strings.ToLower(attrStr), strings.ToLower(f.Value))
	case ">", "<", ">=", "<=":
		a, errA := strconv.ParseFloat(attrStr, 64)
		b, errB := strconv.ParseFloat(f.Value, 64)
		if errA != nil || errB != nil {
			return false
		}
		switch f.Op {
		case ">":
			return a > b
		case "<":
			return a < b
		case ">=":
			return a >= b
		case "<=":
			return a <= b
		}
	}
	return false
}

// isEmbeddingAttr reports whether an attribute name is one of the embedding
// vectors kg used to write into entity files. Vectors now live in the sidecar
// (P2), but corpora written before that still carry these, and every read path
// hides them by default.
func isEmbeddingAttr(name string) bool {
	return strings.HasSuffix(name, "_embedding")
}

// StripEmbeddings removes legacy embedding attributes from an entity in place.
func StripEmbeddings(e *Entity) {
	for k := range e.Attributes {
		if isEmbeddingAttr(k) {
			delete(e.Attributes, k)
		}
	}
}
