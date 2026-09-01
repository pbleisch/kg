package output

import (
	"bytes"
	"strings"
	"testing"

	"github.com/pbleisch/kg/pkg/store"
)

// A multi-valued attribute rendered with %v comes out as
// `[map[source:… text:…]]`, which is Go's debug formatting and not a value a
// parser can read (P6).
func TestToonRendersListValues(t *testing.T) {
	e := &store.Entity{ID: "concept:flow", Type: "concept", Attributes: map[string]any{
		"categories": []any{"design", "mechanic"},
		"evidence": []any{
			map[string]any{store.ItemSourceKey: "source:mda", store.ItemTextKey: "a quote, with a comma"},
		},
		"name": "Flow",
	}}
	var buf bytes.Buffer
	if err := (&toonFormatter{}).FormatEntity(&buf, e); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, "map[") {
		t.Errorf("no Go debug formatting may reach the output:\n%s", out)
	}
	for _, want := range []string{
		"categories[2]: design,mechanic",
		"evidence[1]{text,source}:",
		`  "a quote, with a comma",source:mda`,
		"name: Flow",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// A list in a table cell is one field: its items are joined with the separator
// relations already use inside a cell, and the cell is quoted when an item
// carries the row separator.
func TestToonTabularKeepsAListInOneCell(t *testing.T) {
	entities := []*store.Entity{{
		ID: "concept:flow", Type: "concept",
		Attributes: map[string]any{"categories": []any{"design", "mechanic"}},
	}, {
		ID: "concept:quest", Type: "concept",
		Attributes: map[string]any{"categories": []any{"a task, or a reward"}},
	}}
	var buf bytes.Buffer
	if err := (&toonFormatter{}).FormatEntities(&buf, entities); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "concept:flow,concept,design;mechanic\n") {
		t.Errorf("a list cell joins its items with ';':\n%s", out)
	}
	// An item carrying the row separator is quoted, so the row still has as
	// many fields as the header declares.
	if !strings.Contains(out, `"a task, or a reward"`) {
		t.Errorf("an item holding the separator must be quoted:\n%s", out)
	}
	if strings.Contains(out, "map[") {
		t.Errorf("no Go debug formatting may reach the output:\n%s", out)
	}
}

func TestHumanRendersListValuesWithTheirSources(t *testing.T) {
	e := &store.Entity{ID: "concept:flow", Type: "concept", Attributes: map[string]any{
		"evidence": []any{
			map[string]any{store.ItemSourceKey: "source:mda", store.ItemTextKey: "a quote"},
			"typed by hand",
		},
	}}
	var buf bytes.Buffer
	if err := (&humanFormatter{}).FormatEntity(&buf, e); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "- a quote  [source:mda]") {
		t.Errorf("an attributed item must show its source:\n%s", out)
	}
	if !strings.Contains(out, "- typed by hand\n") {
		t.Errorf("an unattributed item must show as itself:\n%s", out)
	}
}

func TestQuoteField(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"plain", "plain"},
		{"has,comma", `"has,comma"`},
		{`has"quote`, `"has""quote"`},
	} {
		if got := quoteField(tc.in); got != tc.want {
			t.Errorf("quoteField(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
