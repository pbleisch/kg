package output

import (
	"bytes"
	"strings"
	"testing"

	"github.com/pbleisch/kg/pkg/store"
)

func TestHumanFormatEntity(t *testing.T) {
	f := NewFormatter(FormatHuman)
	e := &store.Entity{
		ID:   "person:alice",
		Type: "Person",
		Attributes: map[string]any{
			"name":  "Alice Chen",
			"role":  "Engineer",
			"email": "alice@example.com",
		},
		Relations: map[string][]string{
			"worksOn":   {"project:atlas"},
			"reportsTo": {"person:bob"},
		},
	}

	var buf bytes.Buffer
	if err := f.FormatEntity(&buf, e); err != nil {
		t.Fatal(err)
	}

	got := buf.String()

	// Check header.
	if !strings.Contains(got, "═══ Person: person:alice ═══") {
		t.Errorf("missing header, got:\n%s", got)
	}

	// Check attributes are present.
	for _, want := range []string{"name:", "Alice Chen", "role:", "Engineer", "email:", "alice@example.com"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in output:\n%s", want, got)
		}
	}

	// Check relations section.
	if !strings.Contains(got, "Relations:") {
		t.Errorf("missing Relations section, got:\n%s", got)
	}
	if !strings.Contains(got, "worksOn → project:atlas") {
		t.Errorf("missing worksOn relation, got:\n%s", got)
	}
	if !strings.Contains(got, "reportsTo → person:bob") {
		t.Errorf("missing reportsTo relation, got:\n%s", got)
	}
}

func TestHumanFormatEntityNoRelations(t *testing.T) {
	f := NewFormatter(FormatHuman)
	e := &store.Entity{
		ID:         "project:atlas",
		Type:       "Project",
		Attributes: map[string]any{"name": "Atlas"},
	}

	var buf bytes.Buffer
	if err := f.FormatEntity(&buf, e); err != nil {
		t.Fatal(err)
	}

	got := buf.String()
	if strings.Contains(got, "Relations:") {
		t.Errorf("should not have Relations section for entity with no relations, got:\n%s", got)
	}
}

func TestHumanFormatEntities(t *testing.T) {
	f := NewFormatter(FormatHuman)
	entities := []*store.Entity{
		{ID: "person:alice", Type: "Person", Attributes: map[string]any{"name": "Alice Chen"}},
		{ID: "person:bob", Type: "Person", Attributes: map[string]any{"name": "Bob Smith"}},
	}

	var buf bytes.Buffer
	if err := f.FormatEntities(&buf, entities); err != nil {
		t.Fatal(err)
	}

	got := buf.String()
	if !strings.Contains(got, "Person: person:alice") {
		t.Errorf("missing alice header, got:\n%s", got)
	}
	if !strings.Contains(got, "Person: person:bob") {
		t.Errorf("missing bob header, got:\n%s", got)
	}
	if !strings.Contains(got, "(2 entities)") {
		t.Errorf("missing entity count, got:\n%s", got)
	}
}

func TestHumanFormatEntitiesEmpty(t *testing.T) {
	f := NewFormatter(FormatHuman)
	var buf bytes.Buffer
	if err := f.FormatEntities(&buf, nil); err != nil {
		t.Fatal(err)
	}

	got := buf.String()
	if !strings.Contains(got, "No results.") {
		t.Errorf("expected 'No results.', got:\n%s", got)
	}
}

func TestHumanFormatError(t *testing.T) {
	f := NewFormatter(FormatHuman)
	var buf bytes.Buffer
	if err := f.FormatError(&buf, "not_found", "entity not found", []string{"person:alice", "person:bob"}); err != nil {
		t.Fatal(err)
	}

	got := buf.String()
	if !strings.Contains(got, "Error [not_found]: entity not found") {
		t.Errorf("missing error line, got:\n%s", got)
	}
	if !strings.Contains(got, "Did you mean:") {
		t.Errorf("missing suggestions header, got:\n%s", got)
	}
	if !strings.Contains(got, "• person:alice") {
		t.Errorf("missing suggestion, got:\n%s", got)
	}
}

func TestHumanFormatErrorNoSuggestions(t *testing.T) {
	f := NewFormatter(FormatHuman)
	var buf bytes.Buffer
	if err := f.FormatError(&buf, "parse_error", "invalid syntax", nil); err != nil {
		t.Fatal(err)
	}

	got := buf.String()
	if !strings.Contains(got, "Error [parse_error]: invalid syntax") {
		t.Errorf("missing error line, got:\n%s", got)
	}
	if strings.Contains(got, "Did you mean:") {
		t.Errorf("should not have suggestions section, got:\n%s", got)
	}
}
