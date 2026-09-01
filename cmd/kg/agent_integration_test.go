//go:build integration

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pbleisch/kg/pkg/store"
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

	for _, want := range []string{"entity_count:", "schema_types", "vectors:", "embedder:"} {
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

// breakOneIdentity makes a corpus that kg refuses, in the exact class
// ADR-0029 defines: the file's declared id disagrees with its location, and
// the location is the identity.
func breakOneIdentity(t *testing.T, dir string) {
	t.Helper()
	path := filepath.Join(dir, "entities", "person", "bob.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading bob: %v", err)
	}
	broken := strings.Replace(string(data), "id: person:bob", "id: person:robert", 1)
	if broken == string(data) {
		t.Fatal("test setup: bob.yaml did not declare the id we meant to break")
	}
	if err := os.WriteFile(path, []byte(broken), 0o644); err != nil {
		t.Fatalf("writing broken bob: %v", err)
	}
}

// The agent-facing surface reported `healthy: true` on a corpus `kg validate`,
// `kg find` and `kg stats` all refuse. It walked with a read that refuses,
// dropped the refusal, and validated the empty list that came back — so the
// agent following its own SKILL.md was told to proceed into a graph whose next
// read would fail. This is that bug, and it must stay fixed.
func TestAgentCheckReportsARefusedCorpus(t *testing.T) {
	dir := initKGWithSchema(t)
	for _, id := range []string{"alice", "bob"} {
		if _, err := runCmd(t, "create", "person", id, "name="+id, "--dir="+dir); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}
	breakOneIdentity(t, dir)

	// The human surface refuses.
	humanOut, humanErr := runCmd(t, "validate", "--dir="+dir)
	if humanErr == nil {
		t.Fatalf("test setup: kg validate accepted a corpus it should refuse: %q", humanOut)
	}
	if _, refused := store.LoadRefused(humanErr); !refused {
		t.Fatalf("kg validate did not refuse the load: %v", humanErr)
	}

	// The agent surface must refuse it the same way, with the same error
	// type — that is what carries exit code 2 out of main().
	out, err := runCmd(t, "agent", "check", "--dir="+dir)
	if err == nil {
		t.Fatalf("kg agent check reported success on a refused corpus: %q", out)
	}
	if _, refused := store.LoadRefused(err); !refused {
		t.Errorf("kg agent check did not return a *store.LoadError, so it will not exit 2: %v", err)
	}
	if !strings.Contains(out, "healthy: false") {
		t.Errorf("expected healthy: false, got: %q", out)
	}
	if !strings.Contains(out, "id_disagreement") {
		t.Errorf("expected the finding to be named, got: %q", out)
	}
}

// `kg agent status` reported entity_count: 0 on the same corpus, which is what
// an empty graph looks like. A refusal is never a miss.
func TestAgentStatusReportsARefusedCorpus(t *testing.T) {
	dir := initKGWithSchema(t)
	for _, id := range []string{"alice", "bob"} {
		if _, err := runCmd(t, "create", "person", id, "name="+id, "--dir="+dir); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}
	breakOneIdentity(t, dir)

	out, err := runCmd(t, "agent", "status", "--dir="+dir)
	if err == nil {
		t.Fatalf("kg agent status reported success on a refused corpus: %q", out)
	}
	if _, refused := store.LoadRefused(err); !refused {
		t.Errorf("kg agent status did not return a *store.LoadError: %v", err)
	}
	if !strings.Contains(out, "graph: refused") {
		t.Errorf("expected the status to say the graph is refused, got: %q", out)
	}
	// Still orienting: an agent facing a broken graph needs the dir and the
	// schema types to repair it.
	if !strings.Contains(out, "schema_types") {
		t.Errorf("status dropped its orientation on a refused corpus: %q", out)
	}
}

// A content finding is advised, never refused (P3, ADR-0028). The agent
// surface must not be stricter than the human one in the other direction
// either: an unknown attribute is a warning on both.
func TestAgentCheckAdvisesRatherThanRefusesContent(t *testing.T) {
	dir := initKGWithSchema(t)
	if _, err := runCmd(t, "create", "person", "dave", "name=Dave", "nickname=Davey", "--dir="+dir); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := runCmd(t, "validate", "--dir="+dir); err != nil {
		t.Fatalf("test setup: kg validate refused a content finding: %v", err)
	}
	out, err := runCmd(t, "agent", "check", "--dir="+dir)
	if err != nil {
		t.Fatalf("kg agent check refused what kg validate advised: %v", err)
	}
	if !strings.Contains(out, "healthy: true") {
		t.Errorf("expected healthy: true, got: %q", out)
	}
	if !strings.Contains(out, "advisories") {
		t.Errorf("expected the schema finding to be advised, got: %q", out)
	}
}
