package schema

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/pbleisch/kg/pkg/store"
)

// `type: list` was in the schema language, declared on five real fields over
// 34,091 attribute instances, and both places that would act on it fell
// through to a default meaning "anything goes".
func TestListTypeIsImplemented(t *testing.T) {
	for _, tc := range []struct {
		name string
		val  any
		ok   bool
	}{
		{"a list is a list", []any{"a", "b"}, true},
		{"a bare scalar is a one-element list (OKF §5.2)", "a", true},
		{"a bare mapping is a one-element list", map[string]any{"text": "a"}, true},
	} {
		if got := checkAttrType(TypeList, tc.val); got != tc.ok {
			t.Errorf("%s: checkAttrType(list, %v) = %v, want %v", tc.name, tc.val, got, tc.ok)
		}
	}
	// The reportable direction is the other one: a list under a field the
	// schema calls a string.
	if checkAttrType("string", []any{"a", "b"}) {
		t.Error("a list at a string-declared field must be reported")
	}
}

// The one prohibition the design is specific about.
func TestParseAttrValueNeverSplitsOnAComma(t *testing.T) {
	got, err := parseAttrValue(TypeList, "design,mechanic")
	if err != nil {
		t.Fatal(err)
	}
	want := []any{"design,mechanic"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseAttrValue(list, %q) = %#v, want %#v — splitting would shred every value holding a separator",
			"design,mechanic", got, want)
	}
}

func TestCoerceAttrsMakesAListFromAString(t *testing.T) {
	kgDir := t.TempDir()
	dir := filepath.Join(kgDir, "schema")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "type: concept\nattributes:\n  name:\n    type: string\n  tags:\n    type: list\n"
	if err := os.WriteFile(filepath.Join(dir, "concept.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := CoerceAttrs(kgDir, "concept", map[string]string{"name": "Flow", "tags": "a,b"})
	if err != nil {
		t.Fatal(err)
	}
	if out["name"] != "Flow" {
		t.Errorf("name = %v, want the string unchanged", out["name"])
	}
	if !reflect.DeepEqual(out["tags"], []any{"a,b"}) {
		t.Errorf("tags = %#v, want a one-element list", out["tags"])
	}
}

func TestFieldKinds(t *testing.T) {
	s := &Schema{Type: "concept", Attributes: map[string]AttrDef{
		"name":     {Type: "string"},
		"evidence": {Type: TypeList},
	}}
	k := KindsOf(s)
	if !k.IsList("evidence") {
		t.Error("evidence is declared a list")
	}
	if k.IsList("name") || k.IsList("nothing") {
		t.Error("only a declared list is a list")
	}
	if !k.Any() {
		t.Error("Any must see the one list attribute")
	}
	// A type with no schema has no list fields: an undeclared field is a
	// scalar, and content is advised rather than enforced.
	if KindsOf(nil).IsList("evidence") {
		t.Error("no schema means no multi-valued fields")
	}
	if LoadFieldKinds(t.TempDir(), "nosuchtype").Any() {
		t.Error("a missing schema is a state, not a failure")
	}
	// The cache answers the same question as the uncached read.
	_ = store.ToSnakeCase("Concept")
}
