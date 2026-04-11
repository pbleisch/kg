package mcp

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestInitialize(t *testing.T) {
	srv := NewServer("/nonexistent")
	input := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n"
	var buf bytes.Buffer
	if err := srv.Run(strings.NewReader(input), &buf); err != nil {
		t.Fatal(err)
	}
	var resp response
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
}

func TestToolsList(t *testing.T) {
	srv := NewServer("/nonexistent")
	input := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}` + "\n"
	var buf bytes.Buffer
	if err := srv.Run(strings.NewReader(input), &buf); err != nil {
		t.Fatal(err)
	}
	var resp struct {
		Result toolsListResult `json:"result"`
	}
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Result.Tools) != 13 {
		t.Fatalf("expected 13 tools, got %d", len(resp.Result.Tools))
	}
	names := make(map[string]bool)
	for _, tool := range resp.Result.Tools {
		names[tool.Name] = true
	}
	for _, want := range []string{"kg_get", "kg_find", "kg_back", "kg_path", "kg_create", "kg_set", "kg_delete", "kg_link", "kg_unlink", "kg_types", "kg_describe", "kg_stats", "kg_validate"} {
		if !names[want] {
			t.Errorf("missing tool: %s", want)
		}
	}
}

func TestNotificationIgnored(t *testing.T) {
	srv := NewServer("/nonexistent")
	input := `{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n"
	var buf bytes.Buffer
	if err := srv.Run(strings.NewReader(input), &buf); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Fatalf("expected no output for notification, got: %s", buf.String())
	}
}

func TestUnknownMethod(t *testing.T) {
	srv := NewServer("/nonexistent")
	input := `{"jsonrpc":"2.0","id":1,"method":"unknown/method","params":{}}` + "\n"
	var buf bytes.Buffer
	if err := srv.Run(strings.NewReader(input), &buf); err != nil {
		t.Fatal(err)
	}
	var resp response
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if resp.Error.Code != -32601 {
		t.Fatalf("expected code -32601, got %d", resp.Error.Code)
	}
}
