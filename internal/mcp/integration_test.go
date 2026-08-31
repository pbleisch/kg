//go:build integration

package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// sendRPC sends a JSON-RPC request and decodes the response.
func sendRPC(t *testing.T, w io.Writer, r *bufio.Scanner, id int, method string, params any) response {
	t.Helper()
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
	}
	if params != nil {
		p, err := json.Marshal(params)
		if err != nil {
			t.Fatalf("marshal params: %v", err)
		}
		req["params"] = json.RawMessage(p)
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	data = append(data, '\n')
	if _, err := w.Write(data); err != nil {
		t.Fatalf("write request: %v", err)
	}

	if !r.Scan() {
		t.Fatalf("no response for %s: %v", method, r.Err())
	}
	var resp response
	if err := json.Unmarshal(r.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	return resp
}

// toolCallResultText extracts the text content from a tool call response.
func toolCallResultText(t *testing.T, resp response) (string, bool) {
	t.Helper()
	raw, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var tcr toolCallResult
	if err := json.Unmarshal(raw, &tcr); err != nil {
		t.Fatalf("unmarshal toolCallResult: %v", err)
	}
	if len(tcr.Content) == 0 {
		t.Fatal("no content in tool call result")
	}
	return tcr.Content[0].Text, tcr.IsError
}

// setupKGDir creates a temp kg directory with test entity YAML files.
func setupKGDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	entities := map[string]string{
		"entities/person/alice.yaml": `id: "person:alice"
type: person
attributes:
  name: Alice Chen
  role: Engineer
`,
		"entities/person/bob.yaml": `id: "person:bob"
type: person
attributes:
  name: Bob Smith
  role: Manager
`,
		"entities/project/atlas.yaml": `id: "project:atlas"
type: project
attributes:
  name: Atlas
  status: active
`,
	}

	schemas := map[string]string{
		"schema/person.yaml": `type: person
description: A person entity
attributes:
  name:
    type: string
    required: true
  role:
    type: string
relations: {}
`,
		"schema/project.yaml": `type: project
description: A project entity
attributes:
  name:
    type: string
    required: true
  status:
    type: string
relations: {}
`,
	}

	for relPath, content := range entities {
		fullPath := filepath.Join(dir, relPath)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}
	}
	for relPath, content := range schemas {
		fullPath := filepath.Join(dir, relPath)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}
	}
	return dir
}

// startServer creates an MCP server and returns the writer/scanner for communication.
// The server runs in a goroutine; closing stdinW triggers shutdown.
func startServer(t *testing.T, kgDir string) (stdinW io.WriteCloser, scanner *bufio.Scanner, done chan error) {
	t.Helper()
	stdinR, stdinWPipe := io.Pipe()
	stdoutR, stdoutW := io.Pipe()

	srv := NewServer(kgDir)
	done = make(chan error, 1)
	go func() {
		done <- srv.Run(stdinR, stdoutW)
		stdoutW.Close()
	}()

	return stdinWPipe, bufio.NewScanner(stdoutR), done
}

func TestIntegrationInitialize(t *testing.T) {
	kgDir := setupKGDir(t)
	w, r, done := startServer(t, kgDir)
	defer w.Close()

	resp := sendRPC(t, w, r, 1, "initialize", map[string]any{})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	raw, _ := json.Marshal(resp.Result)
	var result initializeResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal initializeResult: %v", err)
	}
	if result.Capabilities.Tools == nil {
		t.Fatal("expected tools capability to be present")
	}
	if result.ServerInfo.Name != "kg" {
		t.Fatalf("expected server name 'kg', got %q", result.ServerInfo.Name)
	}

	w.Close()
	<-done
}

func TestIntegrationToolsList(t *testing.T) {
	kgDir := setupKGDir(t)
	w, r, done := startServer(t, kgDir)
	defer w.Close()

	resp := sendRPC(t, w, r, 1, "tools/list", map[string]any{})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	raw, _ := json.Marshal(resp.Result)
	var result toolsListResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal toolsListResult: %v", err)
	}

	expected := map[string]bool{
		"kg_get": false, "kg_find": false, "kg_back": false,
		"kg_path": false, "kg_create": false, "kg_set": false,
		"kg_delete": false, "kg_link": false, "kg_unlink": false,
		"kg_types": false, "kg_describe": false, "kg_stats": false,
		"kg_validate": false,
	}
	for _, tool := range result.Tools {
		if _, ok := expected[tool.Name]; ok {
			expected[tool.Name] = true
		}
	}
	for name, found := range expected {
		if !found {
			t.Errorf("missing tool: %s", name)
		}
	}

	w.Close()
	<-done
}

func TestIntegrationKgGet(t *testing.T) {
	kgDir := setupKGDir(t)
	w, r, done := startServer(t, kgDir)
	defer w.Close()

	resp := sendRPC(t, w, r, 1, "tools/call", map[string]any{
		"name":      "kg_get",
		"arguments": map[string]any{"id": "person:alice"},
	})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	text, isErr := toolCallResultText(t, resp)
	if isErr {
		t.Fatalf("tool returned error: %s", text)
	}

	var entity struct {
		ID         string         `json:"id"`
		Type       string         `json:"type"`
		Attributes map[string]any `json:"attributes"`
	}
	if err := json.Unmarshal([]byte(text), &entity); err != nil {
		t.Fatalf("unmarshal entity: %v", err)
	}
	if entity.ID != "person:alice" {
		t.Fatalf("expected id 'person:alice', got %q", entity.ID)
	}
	if entity.Attributes["name"] != "Alice Chen" {
		t.Fatalf("expected name 'Alice Chen', got %v", entity.Attributes["name"])
	}

	w.Close()
	<-done
}

func TestIntegrationKgGetNotFound(t *testing.T) {
	kgDir := setupKGDir(t)
	w, r, done := startServer(t, kgDir)
	defer w.Close()

	resp := sendRPC(t, w, r, 1, "tools/call", map[string]any{
		"name":      "kg_get",
		"arguments": map[string]any{"id": "person:nonexistent"},
	})
	if resp.Error != nil {
		t.Fatalf("unexpected JSON-RPC error: %s", resp.Error.Message)
	}

	text, isErr := toolCallResultText(t, resp)
	if !isErr {
		t.Fatalf("expected tool error for nonexistent entity, got: %s", text)
	}

	w.Close()
	<-done
}

func TestIntegrationKgFind(t *testing.T) {
	kgDir := setupKGDir(t)
	w, r, done := startServer(t, kgDir)
	defer w.Close()

	resp := sendRPC(t, w, r, 1, "tools/call", map[string]any{
		"name":      "kg_find",
		"arguments": map[string]any{"type": "person"},
	})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	text, isErr := toolCallResultText(t, resp)
	if isErr {
		t.Fatalf("tool returned error: %s", text)
	}

	var entities []struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(text), &entities); err != nil {
		t.Fatalf("unmarshal entities: %v", err)
	}
	if len(entities) != 2 {
		t.Fatalf("expected 2 Person entities, got %d", len(entities))
	}

	ids := map[string]bool{}
	for _, e := range entities {
		ids[e.ID] = true
	}
	if !ids["person:alice"] || !ids["person:bob"] {
		t.Fatalf("expected person:alice and person:bob, got %v", ids)
	}

	w.Close()
	<-done
}

func TestIntegrationKgCreate(t *testing.T) {
	kgDir := setupKGDir(t)
	w, r, done := startServer(t, kgDir)
	defer w.Close()

	resp := sendRPC(t, w, r, 1, "tools/call", map[string]any{
		"name": "kg_create",
		"arguments": map[string]any{
			"type":     "person",
			"local_id": "charlie",
			"attributes": map[string]string{
				"name": "Charlie Brown",
				"role": "Designer",
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	text, isErr := toolCallResultText(t, resp)
	if isErr {
		t.Fatalf("tool returned error: %s", text)
	}

	var entity struct {
		ID         string         `json:"id"`
		Type       string         `json:"type"`
		Attributes map[string]any `json:"attributes"`
	}
	if err := json.Unmarshal([]byte(text), &entity); err != nil {
		t.Fatalf("unmarshal entity: %v", err)
	}
	if entity.ID != "person:charlie" {
		t.Fatalf("expected id 'person:charlie', got %q", entity.ID)
	}

	// Verify the YAML file was actually written to disk.
	yamlPath := filepath.Join(kgDir, "entities", "person", "charlie.yaml")
	if _, err := os.Stat(yamlPath); os.IsNotExist(err) {
		t.Fatalf("expected YAML file at %s, but it does not exist", yamlPath)
	}

	// Verify we can read it back via kg_get.
	resp2 := sendRPC(t, w, r, 2, "tools/call", map[string]any{
		"name":      "kg_get",
		"arguments": map[string]any{"id": "person:charlie"},
	})
	text2, isErr2 := toolCallResultText(t, resp2)
	if isErr2 {
		t.Fatalf("failed to read back created entity: %s", text2)
	}

	var readBack struct {
		Attributes map[string]any `json:"attributes"`
	}
	if err := json.Unmarshal([]byte(text2), &readBack); err != nil {
		t.Fatalf("unmarshal readBack: %v", err)
	}
	if readBack.Attributes["name"] != "Charlie Brown" {
		t.Fatalf("expected name 'Charlie Brown', got %v", readBack.Attributes["name"])
	}

	w.Close()
	<-done
}

func TestIntegrationMultipleRequests(t *testing.T) {
	kgDir := setupKGDir(t)
	w, r, done := startServer(t, kgDir)
	defer w.Close()

	// Send initialize, then tools/list, then a tool call in sequence
	// to verify the server handles multiple requests on the same connection.
	resp1 := sendRPC(t, w, r, 1, "initialize", map[string]any{})
	if resp1.Error != nil {
		t.Fatalf("initialize error: %s", resp1.Error.Message)
	}

	// Send notification (no response expected), followed by a real request.
	notif := `{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n"
	if _, err := fmt.Fprint(w, notif); err != nil {
		t.Fatalf("write notification: %v", err)
	}

	resp2 := sendRPC(t, w, r, 2, "tools/list", map[string]any{})
	if resp2.Error != nil {
		t.Fatalf("tools/list error: %s", resp2.Error.Message)
	}

	resp3 := sendRPC(t, w, r, 3, "tools/call", map[string]any{
		"name":      "kg_get",
		"arguments": map[string]any{"id": "project:atlas"},
	})
	text, isErr := toolCallResultText(t, resp3)
	if isErr {
		t.Fatalf("kg_get error: %s", text)
	}

	var entity struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(text), &entity); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if entity.ID != "project:atlas" {
		t.Fatalf("expected 'project:atlas', got %q", entity.ID)
	}

	w.Close()
	<-done
}

func TestIntegrationKgTypes(t *testing.T) {
	kgDir := setupKGDir(t)
	w, r, done := startServer(t, kgDir)
	defer w.Close()

	resp := sendRPC(t, w, r, 1, "tools/call", map[string]any{
		"name":      "kg_types",
		"arguments": map[string]any{},
	})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	text, isErr := toolCallResultText(t, resp)
	if isErr {
		t.Fatalf("tool returned error: %s", text)
	}

	var types []string
	if err := json.Unmarshal([]byte(text), &types); err != nil {
		t.Fatalf("unmarshal types: %v", err)
	}
	if len(types) != 2 {
		t.Fatalf("expected 2 types, got %d: %v", len(types), types)
	}
	if types[0] != "person" || types[1] != "project" {
		t.Fatalf("expected [Person, Project], got %v", types)
	}

	w.Close()
	<-done
}

func TestIntegrationKgDescribe(t *testing.T) {
	kgDir := setupKGDir(t)
	w, r, done := startServer(t, kgDir)
	defer w.Close()

	resp := sendRPC(t, w, r, 1, "tools/call", map[string]any{
		"name":      "kg_describe",
		"arguments": map[string]any{"type": "person"},
	})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	text, isErr := toolCallResultText(t, resp)
	if isErr {
		t.Fatalf("tool returned error: %s", text)
	}

	var result struct {
		Type        string `json:"type"`
		Description string `json:"description"`
		Attributes  []struct {
			Name     string `json:"name"`
			Type     string `json:"type"`
			Required bool   `json:"required"`
		} `json:"attributes"`
	}
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}
	if result.Type != "person" {
		t.Fatalf("expected type 'Person', got %q", result.Type)
	}
	if len(result.Attributes) != 2 {
		t.Fatalf("expected 2 attributes, got %d", len(result.Attributes))
	}
	if result.Attributes[0].Name != "name" || !result.Attributes[0].Required {
		t.Fatalf("expected first attr 'name' (required), got %+v", result.Attributes[0])
	}

	w.Close()
	<-done
}

func TestIntegrationKgValidate(t *testing.T) {
	kgDir := setupKGDir(t)
	w, r, done := startServer(t, kgDir)
	defer w.Close()

	resp := sendRPC(t, w, r, 1, "tools/call", map[string]any{
		"name":      "kg_validate",
		"arguments": map[string]any{},
	})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	text, isErr := toolCallResultText(t, resp)
	if isErr {
		t.Fatalf("tool returned error: %s", text)
	}

	var result struct {
		Valid         bool `json:"valid"`
		TotalEntities int  `json:"total_entities"`
		ErrorCount    int  `json:"error_count"`
	}
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		t.Fatalf("unmarshal validate result: %v", err)
	}
	if !result.Valid {
		t.Fatalf("expected valid=true, got false")
	}
	if result.TotalEntities != 3 {
		t.Fatalf("expected 3 entities, got %d", result.TotalEntities)
	}
	if result.ErrorCount != 0 {
		t.Fatalf("expected 0 errors, got %d", result.ErrorCount)
	}

	w.Close()
	<-done
}
