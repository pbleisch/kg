package output

import (
	"bytes"
	"testing"

	"github.com/pbleisch/kg/pkg/store"
)

func TestTOONFormatEntity(t *testing.T) {
	f := NewFormatter(FormatTOON)
	e := &store.Entity{
		ID:   "person:alice",
		Type: "Person",
		Attributes: map[string]any{
			"name": "Alice Chen",
			"role": "Engineer",
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

	expected := "id: person:alice\n" +
		"type: Person\n" +
		"name: Alice Chen\n" +
		"role: Engineer\n" +
		"reportsTo[1]: person:bob\n" +
		"worksOn[1]: project:atlas\n"

	if buf.String() != expected {
		t.Errorf("got:\n%s\nwant:\n%s", buf.String(), expected)
	}
}

func TestTOONFormatEntities(t *testing.T) {
	f := NewFormatter(FormatTOON)
	entities := []*store.Entity{
		{ID: "person:alice", Type: "Person", Attributes: map[string]any{"name": "Alice Chen", "role": "Engineer"}},
		{ID: "person:bob", Type: "Person", Attributes: map[string]any{"name": "Bob Smith", "role": "Manager"}},
		{ID: "person:charlie", Type: "Person", Attributes: map[string]any{"name": "Charlie Davis", "role": "Designer"}},
	}

	var buf bytes.Buffer
	if err := f.FormatEntities(&buf, entities); err != nil {
		t.Fatal(err)
	}

	expected := "results[3]{id,type,name,role}:\n" +
		"  person:alice,Person,Alice Chen,Engineer\n" +
		"  person:bob,Person,Bob Smith,Manager\n" +
		"  person:charlie,Person,Charlie Davis,Designer\n" +
		"count: 3\n"

	if buf.String() != expected {
		t.Errorf("got:\n%s\nwant:\n%s", buf.String(), expected)
	}
}

func TestTOONFormatError(t *testing.T) {
	f := NewFormatter(FormatTOON)
	var buf bytes.Buffer
	if err := f.FormatError(&buf, "entity_not_found", "No entity with id 'person:notexist'", []string{"person:alice", "person:bob"}); err != nil {
		t.Fatal(err)
	}

	expected := "error: entity_not_found\n" +
		"message: No entity with id 'person:notexist'\n" +
		"suggestions[2]: person:alice,person:bob\n"

	if buf.String() != expected {
		t.Errorf("got:\n%s\nwant:\n%s", buf.String(), expected)
	}
}

func TestTOONFormatErrorNoSuggestions(t *testing.T) {
	f := NewFormatter(FormatTOON)
	var buf bytes.Buffer
	if err := f.FormatError(&buf, "parse_error", "invalid syntax", nil); err != nil {
		t.Fatal(err)
	}

	expected := "error: parse_error\n" +
		"message: invalid syntax\n"

	if buf.String() != expected {
		t.Errorf("got:\n%s\nwant:\n%s", buf.String(), expected)
	}
}
