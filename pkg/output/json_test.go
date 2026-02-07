package output

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/pbleisch/kg/pkg/store"
)

func TestJSONFormatEntity(t *testing.T) {
	f := NewFormatter(FormatJSON)
	e := &store.Entity{
		ID:   "person:alice",
		Type: "Person",
		Attributes: map[string]any{
			"name": "Alice Chen",
		},
		Relations: map[string][]string{
			"worksOn": {"project:atlas"},
		},
	}

	var buf bytes.Buffer
	if err := f.FormatEntity(&buf, e); err != nil {
		t.Fatal(err)
	}

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["id"] != "person:alice" {
		t.Errorf("id = %v, want person:alice", got["id"])
	}
	if got["type"] != "Person" {
		t.Errorf("type = %v, want Person", got["type"])
	}
}

func TestJSONFormatEntities(t *testing.T) {
	f := NewFormatter(FormatJSON)
	entities := []*store.Entity{
		{ID: "person:alice", Type: "Person"},
		{ID: "person:bob", Type: "Person"},
	}

	var buf bytes.Buffer
	if err := f.FormatEntities(&buf, entities); err != nil {
		t.Fatal(err)
	}

	var got []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("len = %d, want 2", len(got))
	}
	if got[0]["id"] != "person:alice" {
		t.Errorf("got[0].id = %v, want person:alice", got[0]["id"])
	}
}

func TestJSONFormatError(t *testing.T) {
	f := NewFormatter(FormatJSON)
	var buf bytes.Buffer
	if err := f.FormatError(&buf, "not_found", "missing", []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["error"] != "not_found" {
		t.Errorf("error = %v, want not_found", got["error"])
	}
	if got["message"] != "missing" {
		t.Errorf("message = %v, want missing", got["message"])
	}
}
