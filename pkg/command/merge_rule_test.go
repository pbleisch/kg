package command

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/pbleisch/kg/pkg/store"
)

func ruleGraph(t *testing.T) string {
	t.Helper()
	kgDir := filepath.Join(t.TempDir(), ".kg")
	for _, d := range []string{"entities", "schema"} {
		if err := os.MkdirAll(filepath.Join(kgDir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	body := "type: concept\nattributes:\n  name:\n    type: string\n  description:\n    type: string\n  evidence:\n    type: list\n"
	if err := os.WriteFile(filepath.Join(kgDir, "schema", "concept.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return kgDir
}

func write(t *testing.T, kgDir, id string, attrs map[string]any) {
	t.Helper()
	e := &store.Entity{ID: id, Type: "concept", Attributes: attrs}
	if err := store.WriteEntity(kgDir, e); err != nil {
		t.Fatal(err)
	}
}

func itemPairs(t *testing.T, kgDir, id, attr string) []string {
	t.Helper()
	e, err := store.ReadEntity(kgDir, id)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, it := range store.DecodeItems(e.Attributes[attr]) {
		out = append(out, it.Source+"|"+it.TextString())
	}
	return out
}

// kg held three answers to "a record claims an existing identity", two of them
// exact opposites: an upsert took the incoming value and a merge kept the
// target's.  ADR-0030 settles it on incoming-wins, and this pins the two verbs
// to the same answer.
func TestUpsertAndMergeAgreeOnTheScalarRule(t *testing.T) {
	kgDir := ruleGraph(t)
	write(t, kgDir, "concept:alpha", map[string]any{"name": "Alpha", "description": "A, the existing one"})
	write(t, kgDir, "concept:beta", map[string]any{"name": "Beta", "description": "B, the incoming one"})

	// upsert: incoming wins.
	upserted, _, err := UpsertEntity(kgDir, &store.Entity{
		ID: "concept:alpha", Type: "concept",
		Attributes: map[string]any{"description": "B, the incoming one"},
	}, UpsertOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if upserted.Attributes["description"] != "B, the incoming one" {
		t.Errorf("upsert: description = %v, want the incoming value", upserted.Attributes["description"])
	}

	// merge: the same answer.  It used to be the opposite one.
	write(t, kgDir, "concept:alpha", map[string]any{"name": "Alpha", "description": "A, the existing one"})
	if _, err := Merge(kgDir, "concept:beta", "concept:alpha", MergeOptions{}); err != nil {
		t.Fatal(err)
	}
	e, err := store.ReadEntity(kgDir, "concept:alpha")
	if err != nil {
		t.Fatal(err)
	}
	if e.Attributes["description"] != "B, the incoming one" {
		t.Errorf("merge: description = %v, want the incoming value — one merge model, not two",
			e.Attributes["description"])
	}
}

// --prefer narrows to the scalar rule, which is the only field kind where a
// preference was ever meaningful.
func TestMergePreferTargetKeepsTheExistingScalar(t *testing.T) {
	kgDir := ruleGraph(t)
	write(t, kgDir, "concept:alpha", map[string]any{"name": "Alpha", "description": "A"})
	write(t, kgDir, "concept:beta", map[string]any{"name": "Beta", "description": "B"})

	if _, err := Merge(kgDir, "concept:beta", "concept:alpha", MergeOptions{Prefer: "target"}); err != nil {
		t.Fatal(err)
	}
	e, err := store.ReadEntity(kgDir, "concept:alpha")
	if err != nil {
		t.Fatal(err)
	}
	if e.Attributes["description"] != "A" {
		t.Errorf("description = %v, want the target's", e.Attributes["description"])
	}
}

// Two entities being merged genuinely were written by different sources, and
// both accounts are true: a multi-valued attribute keeps both rather than
// throwing one away.
func TestMergeConcatenatesMultiValuedAttributes(t *testing.T) {
	kgDir := ruleGraph(t)
	write(t, kgDir, "concept:alpha", map[string]any{
		"name": "Alpha",
		"evidence": []any{
			map[string]any{store.ItemSourceKey: "source:a", store.ItemTextKey: "what a said"},
		},
	})
	write(t, kgDir, "concept:beta", map[string]any{
		"name": "Beta",
		"evidence": []any{
			map[string]any{store.ItemSourceKey: "source:b", store.ItemTextKey: "what b said"},
		},
	})

	if _, err := Merge(kgDir, "concept:beta", "concept:alpha", MergeOptions{}); err != nil {
		t.Fatal(err)
	}
	got := itemPairs(t, kgDir, "concept:alpha", "evidence")
	want := []string{"source:a|what a said", "source:b|what b said"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("evidence = %v, want %v — target-wins used to throw one account away", got, want)
	}
}

// An upsert on an identity nothing holds yet must leave the same bytes as the
// second upsert of the same record, or the first write and the second differ
// for no reason a reader could act on.
func TestFirstUpsertAndSecondLeaveTheSameShape(t *testing.T) {
	kgDir := ruleGraph(t)
	rec := func() *store.Entity {
		return &store.Entity{ID: "concept:flow", Type: "concept", Attributes: map[string]any{
			"name": "Flow", "evidence": []any{"b", "a"},
		}}
	}
	first, _, err := UpsertEntity(kgDir, rec(), UpsertOptions{Contributor: "source:mda"})
	if err != nil {
		t.Fatal(err)
	}
	firstItems := append([]string(nil), itemPairs(t, kgDir, "concept:flow", "evidence")...)
	if _, _, err := UpsertEntity(kgDir, rec(), UpsertOptions{Contributor: "source:mda"}); err != nil {
		t.Fatal(err)
	}
	secondItems := itemPairs(t, kgDir, "concept:flow", "evidence")
	if !reflect.DeepEqual(firstItems, secondItems) {
		t.Errorf("first = %v, second = %v", firstItems, secondItems)
	}
	// Sorted by (source, text), so two rebuilds of one corpus are diffable.
	if !reflect.DeepEqual(firstItems, []string{"source:mda|a", "source:mda|b"}) {
		t.Errorf("items = %v, want them ordered", firstItems)
	}
	_ = first
}
