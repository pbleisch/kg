package merge

import (
	"reflect"
	"testing"

	"github.com/pbleisch/kg/pkg/schema"
	"github.com/pbleisch/kg/pkg/store"
)

// listKinds declares one multi-valued attribute, which is the only way a field
// becomes one (ADR-0030).
func listKinds() schema.FieldKinds {
	return schema.KindsOf(&schema.Schema{Type: "concept", Attributes: map[string]schema.AttrDef{
		"description": {Type: "string"},
		"evidence":    {Type: schema.TypeList},
	}})
}

func item(text, src string) store.Item { return store.Item{Text: text, Source: src} }

func pairs(items []store.Item) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Source+"|"+it.TextString())
	}
	return out
}

func TestScalarIncomingWins(t *testing.T) {
	got, w := Scalar("description", "old", "new", Options{})
	if got != "new" {
		t.Errorf("value = %v, want the incoming one", got)
	}
	if w == nil || w.Code != "attribute_overwritten" {
		t.Errorf("overwriting a different value must be reported, got %+v", w)
	}

	// Nothing there is not an overwrite.
	if _, w := Scalar("description", nil, "new", Options{}); w != nil {
		t.Errorf("filling an empty slot is not an overwrite, got %+v", w)
	}
	// The same value is not a loss.
	if _, w := Scalar("description", "same", "same", Options{}); w != nil {
		t.Errorf("rewriting the same value is not an overwrite, got %+v", w)
	}
	// The preference that narrows to this field kind.
	if got, _ := Scalar("description", "old", "new", Options{PreferExisting: true}); got != "old" {
		t.Errorf("value = %v, want the existing one under PreferExisting", got)
	}
}

// A scalar comparison must not panic when either side is multi-valued, which
// after this change either side can be.
func TestScalarComparesListsWithoutPanicking(t *testing.T) {
	if _, w := Scalar("x", []any{"a"}, []any{"a"}, Options{}); w != nil {
		t.Errorf("equal lists are not an overwrite, got %+v", w)
	}
	if _, w := Scalar("x", []any{"a"}, []any{"b"}, Options{}); w == nil {
		t.Error("different lists at a scalar field are an overwrite")
	}
}

func TestItemsReplaceByContributor(t *testing.T) {
	existing := []store.Item{
		item("a's old reading", "source:a"),
		item("b's reading", "source:b"),
		item("typed by hand", ""),
	}
	incoming := []store.Item{item("a's new reading", "")}

	got := Items(existing, incoming, Options{Contributor: "source:a", Mode: Replace})
	want := []string{
		"|typed by hand",
		"source:a|a's new reading",
		"source:b|b's reading",
	}
	if !reflect.DeepEqual(pairs(got), want) {
		t.Errorf("items = %v, want %v", pairs(got), want)
	}
}

// Append is the fold's mode: two claims neither of which is the prior state.
func TestItemsAppendKeepsBothClaims(t *testing.T) {
	existing := []store.Item{item("first section", "source:a")}
	incoming := []store.Item{item("second section", "")}

	got := Items(existing, incoming, Options{Contributor: "source:a", Mode: Append})
	want := []string{"source:a|first section", "source:a|second section"}
	if !reflect.DeepEqual(pairs(got), want) {
		t.Errorf("items = %v, want %v — a fold must not delete the first record's items", pairs(got), want)
	}
}

// With no contributor nothing is replaced, which is what makes a hand-authored
// item permanent.
func TestItemsWithNoContributorReplaceNothing(t *testing.T) {
	existing := []store.Item{item("already here", "")}
	got := Items(existing, []store.Item{item("also here", "")}, Options{Mode: Replace})
	if !reflect.DeepEqual(pairs(got), []string{"|already here", "|also here"}) {
		t.Errorf("items = %v", pairs(got))
	}
}

func TestItemsDedupeAndOrder(t *testing.T) {
	got := Items(nil, []store.Item{
		item("b", "source:z"), item("a", "source:z"), item("a", "source:z"), item("c", "source:a"),
	}, Options{Mode: Replace})
	want := []string{"source:a|c", "source:z|a", "source:z|b"}
	if !reflect.DeepEqual(pairs(got), want) {
		t.Errorf("items = %v, want %v — deduplicated on (source, text) and ordered by it", pairs(got), want)
	}
}

func TestAttributesByFieldKind(t *testing.T) {
	existing := map[string]any{
		"description": "the old one",
		"evidence":    []any{map[string]any{"source": "source:b", "text": "b said this"}},
	}
	incoming := map[string]any{
		"description": "the new one",
		"evidence":    "one sentence, with a comma",
	}

	got, warns := Attributes(existing, incoming, listKinds(),
		Options{Contributor: "source:a", Mode: Replace})

	if got["description"] != "the new one" {
		t.Errorf("description = %v, want the incoming one", got["description"])
	}
	ev := pairs(store.DecodeItems(got["evidence"]))
	want := []string{"source:a|one sentence, with a comma", "source:b|b said this"}
	if !reflect.DeepEqual(ev, want) {
		t.Errorf("evidence = %v, want %v", ev, want)
	}
	if len(warns) != 1 || warns[0].Code != "attribute_overwritten" {
		t.Errorf("warnings = %+v, want one attribute_overwritten", warns)
	}
	// Neither input is modified.
	if existing["description"] != "the old one" {
		t.Error("Attributes must not modify its inputs")
	}
}

func TestUnattributedWarningOnlyWhenItemsReallyLandUnattributed(t *testing.T) {
	kinds := listKinds()
	// No contributor, bare items: reported.
	_, warns := Attributes(nil, map[string]any{"evidence": []any{"a note"}}, kinds, Options{Mode: Replace})
	if len(warns) != 1 || warns[0].Code != "unattributed_items" {
		t.Errorf("warnings = %+v, want one unattributed_items", warns)
	}
	// No contributor, but every item names its own source: nothing to say.
	_, warns = Attributes(nil, map[string]any{
		"evidence": []any{map[string]any{"source": "source:b", "text": "b said this"}},
	}, kinds, Options{Mode: Replace})
	if len(warns) != 0 {
		t.Errorf("warnings = %+v, want none", warns)
	}
	// A fold is not the write that lands them.
	_, warns = Attributes(nil, map[string]any{"evidence": []any{"a note"}}, kinds, Options{Mode: Append})
	if len(warns) != 0 {
		t.Errorf("warnings = %+v, want none from an intermediate fold", warns)
	}
}

func TestRelationsUnion(t *testing.T) {
	existing := map[string][]string{"sourced_from": {"source:a"}}
	incoming := map[string][]string{"sourced_from": {"source:a", "source:b"}, "uses": {"concept:x"}}

	got, added := Relations(existing, incoming)
	if added != 2 {
		t.Errorf("added = %d, want 2", added)
	}
	if !reflect.DeepEqual(got["sourced_from"], []string{"source:a", "source:b"}) {
		t.Errorf("sourced_from = %v — re-submitting a target is a no-op", got["sourced_from"])
	}
	if !reflect.DeepEqual(got["uses"], []string{"concept:x"}) {
		t.Errorf("uses = %v", got["uses"])
	}
	if len(existing["sourced_from"]) != 1 {
		t.Error("Relations must not modify its inputs")
	}
}
