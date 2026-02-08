//go:build integration

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// initKGWithSchema sets up a kg directory with a person schema.
func initKGWithSchema(t *testing.T) string {
	t.Helper()
	dir := initKG(t)
	schemaContent := `type: person
description: A person
attributes:
  name:
    type: string
    required: true
relations:
  knows:
    target: person
    cardinality: many
`
	if err := os.WriteFile(filepath.Join(dir, "schema", "person.yaml"), []byte(schemaContent), 0o644); err != nil {
		t.Fatalf("writing schema: %v", err)
	}
	return dir
}

func TestAgentStatus(t *testing.T) {
	dir := initKGWithSchema(t)

	// Create an entity so we have something to count.
	_, err := runCmd(t, "create", "person", "alice", "name=Alice", "--dir="+dir)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	out, err := runCmd(t, "agent", "status", "--dir="+dir)
	if err != nil {
		t.Fatalf("agent status: %v", err)
	}

	for _, want := range []string{"entity_count:", "schema_types", "index_status:", "daemon_status:"} {
		if !strings.Contains(out, want) {
			t.Errorf("agent status missing %q, got:\n%s", want, out)
		}
	}
}

func TestAgentStatusJSON(t *testing.T) {
	dir := initKGWithSchema(t)

	out, err := runCmd(t, "agent", "status", "--json", "--dir="+dir)
	if err != nil {
		t.Fatalf("agent status --json: %v", err)
	}

	if !strings.Contains(out, `"entity_count"`) {
		t.Errorf("JSON output missing entity_count, got:\n%s", out)
	}
}

func TestAgentSchema(t *testing.T) {
	dir := initKGWithSchema(t)

	out, err := runCmd(t, "agent", "schema", "--dir="+dir)
	if err != nil {
		t.Fatalf("agent schema: %v", err)
	}

	// Output may be TOON or JSON depending on flag state; check for
	// content that appears in either format.
	for _, want := range []string{"person", "name", "knows"} {
		if !strings.Contains(out, want) {
			t.Errorf("agent schema missing %q, got:\n%s", want, out)
		}
	}
}

func TestAgentSchemaJSON(t *testing.T) {
	dir := initKGWithSchema(t)

	out, err := runCmd(t, "agent", "schema", "--json", "--dir="+dir)
	if err != nil {
		t.Fatalf("agent schema --json: %v", err)
	}

	for _, want := range []string{`"type"`, `"attributes"`, `"relations"`} {
		if !strings.Contains(out, want) {
			t.Errorf("JSON output missing %q, got:\n%s", want, out)
		}
	}
}

func TestAgentSkill(t *testing.T) {
	dir := initKGWithSchema(t)

	out, err := runCmd(t, "agent", "skill", "--dir="+dir)
	if err != nil {
		t.Fatalf("agent skill: %v", err)
	}

	for _, want := range []string{"name: kg", "kg create", "kg get", "person"} {
		if !strings.Contains(out, want) {
			t.Errorf("agent skill missing %q, got:\n%s", want, out)
		}
	}
}

func TestAgentSkillGitHub(t *testing.T) {
	dir := initKGWithSchema(t)
	tmpDir := t.TempDir()

	// Run from tmpDir so skill file is written there.
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	_, err := runCmd(t, "agent", "skill", "--github", "--dir="+dir)
	if err != nil {
		t.Fatalf("agent skill --github: %v", err)
	}

	skillPath := filepath.Join(tmpDir, ".github", "skills", "kg", "SKILL.md")
	if _, err := os.Stat(skillPath); os.IsNotExist(err) {
		t.Fatalf("expected skill file at %s", skillPath)
	}
}

func TestAgentExamples(t *testing.T) {
	dir := initKGWithSchema(t)

	out, err := runCmd(t, "agent", "examples", "--dir="+dir)
	if err != nil {
		t.Fatalf("agent examples: %v", err)
	}

	for _, want := range []string{"kg create", "kg get", "kg find", "kg validate", "kg delete"} {
		if !strings.Contains(out, want) {
			t.Errorf("agent examples missing %q, got:\n%s", want, out)
		}
	}
}

func TestAgentMCPConfig(t *testing.T) {
	dir := initKGWithSchema(t)

	out, err := runCmd(t, "agent", "mcp-config", "--dir="+dir)
	if err != nil {
		t.Fatalf("agent mcp-config: %v", err)
	}

	for _, want := range []string{`"command"`, `"args"`, `"mcp"`, `"serve"`} {
		if !strings.Contains(out, want) {
			t.Errorf("mcp-config missing %q, got:\n%s", want, out)
		}
	}
}

func TestAgentMCPConfigGitHub(t *testing.T) {
	dir := initKGWithSchema(t)

	out, err := runCmd(t, "agent", "mcp-config", "--github", "--dir="+dir)
	if err != nil {
		t.Fatalf("agent mcp-config --github: %v", err)
	}

	if !strings.Contains(out, `"servers"`) {
		t.Errorf("GitHub config missing servers key, got:\n%s", out)
	}
}

func TestAgentMCPConfigClaude(t *testing.T) {
	dir := initKGWithSchema(t)

	out, err := runCmd(t, "agent", "mcp-config", "--claude", "--dir="+dir)
	if err != nil {
		t.Fatalf("agent mcp-config --claude: %v", err)
	}

	// Check for Claude-specific key or generic MCP config.
	if !strings.Contains(out, `"mcpServers"`) && !strings.Contains(out, `"command"`) {
		t.Errorf("Claude config missing expected keys, got:\n%s", out)
	}
}

func TestAgentCheck(t *testing.T) {
	dir := initKGWithSchema(t)

	_, err := runCmd(t, "create", "person", "alice", "name=Alice", "--dir="+dir)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	out, err := runCmd(t, "agent", "check", "--dir="+dir)
	// check returns error when daemon not running — expected.
	if err == nil {
		t.Log("agent check passed (daemon may be running)")
	}

	if !strings.Contains(out, "healthy") {
		t.Errorf("agent check missing healthy field, got:\n%s", out)
	}
}
