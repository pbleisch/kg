package store

import (
	"fmt"
	"sort"
	"strings"
)

// The two keys an attributed item carries.  An item of a multi-valued
// attribute is either a bare value — hand-authored, or written before anyone
// recorded provenance — or a mapping of what was said and who said it:
//
//	evidence:
//	    - source: source:mda
//	      text: aesthetics are the desirable emotional responses
//
// The shape is OKF's (§5.1): the contributor is named by a stable id rather
// than by a position in some other list, because a positional reference
// misattributes silently the moment the list is reordered.
const (
	ItemTextKey   = "text"
	ItemSourceKey = "source"
)

// An Item is one element of a multi-valued attribute.
//
// Source is the contributor that wrote it, or "" when it carries none.  An
// item with no source is never replaced by any contributor's write and only
// `unset` removes it, so a hand-authored edit survives every re-ingest
// (ADR-0030).
type Item struct {
	Text   any
	Source string

	// extra holds any other keys the item carried, so recognizing an item
	// never costs it bytes.  A producer that adds a key must not thereby make
	// its items unattributable — that would make them accumulate forever
	// under a re-ingest, which is the failure ADR-0030 exists to prevent.
	extra map[string]any
}

// AsList reads a stored value in its multi-valued form.
//
// A slice is itself.  Anything else is a ONE-ELEMENT list holding it — OKF
// §5.2 sets that precedent normatively ("Consumers MUST treat a bare mapping
// as a one-element list"), and it is what makes cardinality content rather
// than structure: a scalar arriving at a list-declared field is coerced, never
// refused (P3, ADR-0028).
//
// A string is never split.  `categories: "design,mechanic"` is one item
// holding that string, which is exactly what it means today; splitting on a
// separator would shred every value that legitimately contains one.
func AsList(v any) []any {
	if v == nil {
		return nil
	}
	switch t := v.(type) {
	case []any:
		return t
	case []string:
		out := make([]any, len(t))
		for i, s := range t {
			out[i] = s
		}
		return out
	}
	return []any{v}
}

// DecodeItem reads one stored element.
//
// A mapping carrying both keys is an attributed item, and any other keys it
// carries are kept and written back unchanged.  Anything else — a bare string,
// a number, a mapping without both keys — is the item itself with no source:
// an unrecognized shape keeps its bytes rather than being reinterpreted.
func DecodeItem(v any) Item {
	m, ok := v.(map[string]any)
	if !ok {
		return Item{Text: v}
	}
	src, hasSrc := m[ItemSourceKey].(string)
	text, hasText := m[ItemTextKey]
	if !hasSrc || !hasText {
		return Item{Text: v}
	}
	var extra map[string]any
	for k, val := range m {
		if k == ItemSourceKey || k == ItemTextKey {
			continue
		}
		if extra == nil {
			extra = make(map[string]any, len(m)-2)
		}
		extra[k] = val
	}
	return Item{Text: text, Source: src, extra: extra}
}

// Encode renders an item back into the form a file holds.
func (it Item) Encode() any {
	if it.Source == "" && it.extra == nil {
		return it.Text
	}
	m := make(map[string]any, len(it.extra)+2)
	for k, v := range it.extra {
		m[k] = v
	}
	m[ItemTextKey] = it.Text
	if it.Source != "" {
		m[ItemSourceKey] = it.Source
	}
	return m
}

// TextString renders an item's content as text.
func (it Item) TextString() string { return scalarText(it.Text) }

// SortKey orders items by (source, text).  Item order has to be fixed or two
// rebuilds of one corpus differ for no reason a reader could act on.
func (it Item) SortKey() string { return it.Source + "\x00" + it.TextString() }

// DecodeItems reads a whole attribute value as items.
func DecodeItems(v any) []Item {
	list := AsList(v)
	out := make([]Item, 0, len(list))
	for _, e := range list {
		out = append(out, DecodeItem(e))
	}
	return out
}

// EncodeItems renders items back into a stored attribute value.
func EncodeItems(items []Item) any {
	out := make([]any, 0, len(items))
	for _, it := range items {
		out = append(out, it.Encode())
	}
	return out
}

// SortItems orders items by (source, text), in place.
func SortItems(items []Item) {
	sort.SliceStable(items, func(a, b int) bool {
		return items[a].SortKey() < items[b].SortKey()
	})
}

// AttrText projects an attribute value onto the text a reader compares,
// embeds or filters on.
//
// Every caller that needs the *words* in an attribute goes through here rather
// than through fmt's %v, which renders a list of attributed items as
// `[map[source:source:mda text:…]]` — not a value any consumer can read, and
// the string every vector would otherwise be built from.
func AttrText(v any) string {
	if v == nil {
		return ""
	}
	// A bare scalar is a one-element list, so it projects onto itself
	// unchanged — which is the path every value in the corpus takes today.
	list := AsList(v)
	parts := make([]string, 0, len(list))
	for _, e := range list {
		if s := DecodeItem(e).TextString(); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "\n")
}

// AttrTexts projects an attribute value onto one string per item, which is
// what a filter matching "any element" compares against.
func AttrTexts(v any) []string {
	list := AsList(v)
	out := make([]string, 0, len(list))
	for _, e := range list {
		out = append(out, DecodeItem(e).TextString())
	}
	return out
}

// scalarText renders one item's content.  A mapping of an unrecognized shape
// still has to render as something; %v is the honest answer there, and it is
// the only place it survives.
func scalarText(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	default:
		return fmt.Sprintf("%v", t)
	}
}
