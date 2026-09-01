package store

import (
	"reflect"
	"testing"
)

func TestAsListCoercesAndNeverSplits(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   any
		want []any
	}{
		{"a slice is itself", []any{"a", "b"}, []any{"a", "b"}},
		{"a string slice widens", []string{"a", "b"}, []any{"a", "b"}},
		{"a scalar is one element", "a", []any{"a"}},
		{"a separator is not a separator", "design,mechanic", []any{"design,mechanic"}},
		{"a mapping is one element", map[string]any{"text": "a"}, []any{map[string]any{"text": "a"}}},
		{"nil is nothing", nil, nil},
	} {
		if got := AsList(tc.in); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: AsList(%#v) = %#v, want %#v", tc.name, tc.in, got, tc.want)
		}
	}
}

func TestItemRoundTrip(t *testing.T) {
	attributed := map[string]any{ItemSourceKey: "source:mda", ItemTextKey: "a quote"}
	it := DecodeItem(attributed)
	if it.Source != "source:mda" || it.Text != "a quote" {
		t.Fatalf("DecodeItem = %+v", it)
	}
	if !reflect.DeepEqual(it.Encode(), attributed) {
		t.Errorf("Encode = %#v, want %#v", it.Encode(), attributed)
	}

	// A bare value is the item, with no source.
	bare := DecodeItem("a note")
	if bare.Source != "" || bare.Text != "a note" {
		t.Fatalf("DecodeItem(bare) = %+v", bare)
	}
	if bare.Encode() != "a note" {
		t.Errorf("a bare item must round-trip as itself, got %#v", bare.Encode())
	}

	// A mapping of some other shape keeps its bytes rather than being
	// reinterpreted as an attributed item.
	other := map[string]any{"text": "a", "note": "b"}
	if got := DecodeItem(other); got.Source != "" || !reflect.DeepEqual(got.Text, other) {
		t.Errorf("an unrecognized mapping must be kept whole, got %+v", got)
	}
}

// An extra key must not cost an item its attribution: an unattributable item
// is never replaced, so it would accumulate on every re-ingest.
func TestItemWithExtraKeysStaysAttributedAndLosesNothing(t *testing.T) {
	in := map[string]any{
		ItemSourceKey: "source:mda", ItemTextKey: "a quote", "confidence": 0.9,
	}
	it := DecodeItem(in)
	if it.Source != "source:mda" || it.Text != "a quote" {
		t.Fatalf("DecodeItem = %+v, want it recognized", it)
	}
	if !reflect.DeepEqual(it.Encode(), in) {
		t.Errorf("Encode = %#v, want every key back: %#v", it.Encode(), in)
	}
}

// The projection that keeps `[map[source:… text:…]]` out of every vector,
// every similarity comparison and every filter.
func TestAttrText(t *testing.T) {
	for _, tc := range []struct {
		in   any
		want string
	}{
		{"a sentence", "a sentence"},
		{nil, ""},
		{[]any{"one", "two"}, "one\ntwo"},
		{[]any{map[string]any{ItemSourceKey: "source:mda", ItemTextKey: "a quote"}}, "a quote"},
		{42, "42"},
	} {
		if got := AttrText(tc.in); got != tc.want {
			t.Errorf("AttrText(%#v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestAttrTextsIsOneStringPerItem(t *testing.T) {
	got := AttrTexts([]any{"one", map[string]any{ItemSourceKey: "s:1", ItemTextKey: "two"}})
	if !reflect.DeepEqual(got, []string{"one", "two"}) {
		t.Errorf("AttrTexts = %#v", got)
	}
	if got := AttrTexts("design,mechanic"); !reflect.DeepEqual(got, []string{"design,mechanic"}) {
		t.Errorf("a scalar is one item, unsplit: %#v", got)
	}
}

// A filter asks a question of a value; a multi-valued value has more than one
// answer and matches if any of them does.
func TestFilterMatchesAnyItem(t *testing.T) {
	e := &Entity{ID: "concept:flow", Attributes: map[string]any{
		"categories": []any{"design", "mechanic"},
		"name":       "Flow",
	}}
	for _, tc := range []struct {
		f    AttrFilter
		want bool
	}{
		{AttrFilter{Attr: "categories", Op: "=", Value: "mechanic"}, true},
		{AttrFilter{Attr: "categories", Op: "=", Value: "design"}, true},
		{AttrFilter{Attr: "categories", Op: "=", Value: "narrative"}, false},
		{AttrFilter{Attr: "categories", Op: "~", Value: "MECH"}, true},
		// "no item equals", not "some item differs" — which a two-item list
		// would satisfy against its own values.
		{AttrFilter{Attr: "categories", Op: "!=", Value: "design"}, false},
		{AttrFilter{Attr: "categories", Op: "!=", Value: "narrative"}, true},
		{AttrFilter{Attr: "name", Op: "=", Value: "Flow"}, true},
	} {
		if got := matchFilter(e, tc.f); got != tc.want {
			t.Errorf("matchFilter(%+v) = %v, want %v", tc.f, got, tc.want)
		}
	}
}
