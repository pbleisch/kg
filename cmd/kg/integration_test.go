//go:build integration

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runCmd executes the root Cobra command with the given args and returns
// any captured output (from commands that use cmd.OutOrStdout) and the error.
// It resets the shared cfg struct between invocations.
func runCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()

	// Reset shared config to defaults.
	cfg.kgDir = ".kg"
	cfg.format = ""
	cfg.quiet = false

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs(args)

	err := rootCmd.Execute()
	return buf.String(), err
}

// initKG runs "kg init" in a temp directory and returns the kg dir path.
// It also ensures the schema directory exists (init creates "schemas/" but
// the schema package reads from "schema/").
func initKG(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), ".kg")
	_, err := runCmd(t, "init", "--dir="+dir)
	if err != nil {
		t.Fatalf("kg init failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "schema"), 0o755); err != nil {
		t.Fatalf("creating schema dir: %v", err)
	}
	return dir
}

func TestInit(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".kg")
	out, err := runCmd(t, "init", "--dir="+dir)
	if err != nil {
		t.Fatalf("kg init failed: %v", err)
	}
	if !strings.Contains(out, "Initialized") {
		t.Errorf("expected 'Initialized' in output, got: %q", out)
	}

	// Verify directory structure.
	for _, sub := range []string{"entities", "schema", ".cache"} {
		info, err := os.Stat(filepath.Join(dir, sub))
		if err != nil {
			t.Errorf("expected %s directory: %v", sub, err)
		} else if !info.IsDir() {
			t.Errorf("expected %s to be a directory", sub)
		}
	}

	// Verify config.yaml exists.
	if _, err := os.Stat(filepath.Join(dir, "config.yaml")); err != nil {
		t.Errorf("expected config.yaml: %v", err)
	}
}

func TestInitIdempotenceBlocked(t *testing.T) {
	dir := initKG(t)

	// Second init should fail (config.yaml already exists).
	_, err := runCmd(t, "init", "--dir="+dir)
	if err == nil {
		t.Fatal("expected error on second init, got nil")
	}
}

func TestCreateEntity(t *testing.T) {
	dir := initKG(t)

	_, err := runCmd(t, "create", "person", "alice", "name=Alice", "--dir="+dir)
	if err != nil {
		t.Fatalf("kg create failed: %v", err)
	}

	// Verify entity file was written.
	entityPath := filepath.Join(dir, "entities", "person", "alice.yaml")
	data, err := os.ReadFile(entityPath)
	if err != nil {
		t.Fatalf("entity file not found: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "person:alice") {
		t.Errorf("entity file missing id, got:\n%s", content)
	}
	if !strings.Contains(content, "Alice") {
		t.Errorf("entity file missing name attribute, got:\n%s", content)
	}
}

func TestGetEntity(t *testing.T) {
	dir := initKG(t)

	_, err := runCmd(t, "create", "person", "alice", "name=Alice", "--dir="+dir)
	if err != nil {
		t.Fatalf("kg create failed: %v", err)
	}

	// get uses the index; create calls sync internally, so it should work.
	_, err = runCmd(t, "get", "person:alice", "--dir="+dir)
	if err != nil {
		t.Fatalf("kg get failed: %v", err)
	}
}

func TestSetAttribute(t *testing.T) {
	dir := initKG(t)

	_, err := runCmd(t, "create", "person", "alice", "name=Alice", "--dir="+dir)
	if err != nil {
		t.Fatalf("kg create failed: %v", err)
	}

	_, err = runCmd(t, "set", "person:alice", "role=engineer", "--dir="+dir)
	if err != nil {
		t.Fatalf("kg set failed: %v", err)
	}

	// Verify attribute was added to file.
	entityPath := filepath.Join(dir, "entities", "person", "alice.yaml")
	data, err := os.ReadFile(entityPath)
	if err != nil {
		t.Fatalf("entity file not found: %v", err)
	}
	if !strings.Contains(string(data), "engineer") {
		t.Errorf("entity file missing role attribute, got:\n%s", string(data))
	}
}

func TestFindEntities(t *testing.T) {
	dir := initKG(t)

	_, err := runCmd(t, "create", "person", "alice", "name=Alice", "--dir="+dir)
	if err != nil {
		t.Fatalf("kg create failed: %v", err)
	}
	_, err = runCmd(t, "create", "person", "bob", "name=Bob", "--dir="+dir)
	if err != nil {
		t.Fatalf("kg create bob failed: %v", err)
	}

	// find uses the index; create syncs internally.
	_, err = runCmd(t, "find", "person", "--dir="+dir)
	if err != nil {
		t.Fatalf("kg find failed: %v", err)
	}
}

func TestTypes(t *testing.T) {
	dir := initKG(t)

	// Create an entity so the type directory exists.
	_, err := runCmd(t, "create", "person", "alice", "name=Alice", "--dir="+dir)
	if err != nil {
		t.Fatalf("kg create failed: %v", err)
	}

	// types lists from schemas, not entities — but it should not error.
	_, err = runCmd(t, "types", "--dir="+dir)
	if err != nil {
		t.Fatalf("kg types failed: %v", err)
	}
}

func TestValidate(t *testing.T) {
	dir := initKG(t)

	_, err := runCmd(t, "create", "person", "alice", "name=Alice", "--dir="+dir)
	if err != nil {
		t.Fatalf("kg create failed: %v", err)
	}

	_, err = runCmd(t, "validate", "--dir="+dir)
	if err != nil {
		t.Fatalf("kg validate failed: %v", err)
	}
}

func TestDeleteEntity(t *testing.T) {
	dir := initKG(t)

	_, err := runCmd(t, "create", "person", "alice", "name=Alice", "--dir="+dir)
	if err != nil {
		t.Fatalf("kg create failed: %v", err)
	}

	_, err = runCmd(t, "delete", "person:alice", "--dir="+dir)
	if err != nil {
		t.Fatalf("kg delete failed: %v", err)
	}

	// Verify entity file was removed.
	entityPath := filepath.Join(dir, "entities", "person", "alice.yaml")
	if _, err := os.Stat(entityPath); !os.IsNotExist(err) {
		t.Errorf("entity file should be deleted, but still exists")
	}
}

func TestSync(t *testing.T) {
	dir := initKG(t)

	_, err := runCmd(t, "create", "person", "alice", "name=Alice", "--dir="+dir)
	if err != nil {
		t.Fatalf("kg create failed: %v", err)
	}

	_, err = runCmd(t, "sync", "--dir="+dir)
	if err != nil {
		t.Fatalf("kg sync failed: %v", err)
	}
}

func TestFullWorkflow(t *testing.T) {
	dir := initKG(t)

	// Create.
	_, err := runCmd(t, "create", "person", "alice", "name=Alice", "--dir="+dir)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Set.
	_, err = runCmd(t, "set", "person:alice", "role=engineer", "--dir="+dir)
	if err != nil {
		t.Fatalf("set: %v", err)
	}

	// Get.
	_, err = runCmd(t, "get", "person:alice", "--dir="+dir)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	// Validate.
	_, err = runCmd(t, "validate", "--dir="+dir)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}

	// Delete.
	_, err = runCmd(t, "delete", "person:alice", "--dir="+dir)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Verify gone.
	_, err = runCmd(t, "get", "person:alice", "--dir="+dir)
	if err == nil {
		t.Fatal("expected error getting deleted entity, got nil")
	}
}

func TestEmbedInit(t *testing.T) {
	dir := initKG(t)

	out, err := runCmd(t, "embed", "--init", "--dir="+dir)
	if err != nil {
		t.Fatalf("embed --init: %v", err)
	}
	if !strings.Contains(out, "Embedding setup scaffolded") {
		t.Errorf("expected scaffolded message, got: %q", out)
	}

	// Verify script was created.
	scriptPath := filepath.Join(dir, "scripts", "embed.py")
	if _, err := os.Stat(scriptPath); err != nil {
		t.Fatalf("embed.py not created: %v", err)
	}

	// Verify requirements.txt was created.
	reqPath := filepath.Join(dir, "scripts", "requirements.txt")
	data, err := os.ReadFile(reqPath)
	if err != nil {
		t.Fatalf("requirements.txt not created: %v", err)
	}
	if !strings.Contains(string(data), "sentence-transformers") {
		t.Errorf("requirements.txt missing sentence-transformers: %q", string(data))
	}

	// Verify config.yaml was updated with embedding section.
	cfgData, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatalf("reading config.yaml: %v", err)
	}
	cfgStr := string(cfgData)
	if !strings.Contains(cfgStr, "embedding:") {
		t.Error("config.yaml missing 'embedding:' section")
	}
	if !strings.Contains(cfgStr, "command:") {
		t.Error("config.yaml missing 'command:' in embedding section")
	}
	if strings.Contains(cfgStr, "auto:") {
		t.Error("config.yaml should not contain 'auto:' field")
	}
	// Commented-out embedding lines should have been removed.
	if strings.Contains(cfgStr, "# embedding:") {
		t.Error("config.yaml still has commented-out embedding block")
	}

	// Verify the generated script is syntactically valid Python.
	scriptData, _ := os.ReadFile(scriptPath)
	script := string(scriptData)
	if !strings.Contains(script, "SentenceTransformer") {
		t.Error("embed.py missing SentenceTransformer reference")
	}
	if !strings.Contains(script, "json.load") {
		t.Error("embed.py missing json.load (stdin reading)")
	}

	// Running --init again should fail (script already exists).
	_, err = runCmd(t, "embed", "--init", "--dir="+dir)
	if err == nil {
		t.Fatal("expected error on second embed --init")
	}
}

func TestEmbedInitScriptSyntax(t *testing.T) {
	dir := initKG(t)
	runCmd(t, "embed", "--init", "--dir="+dir)

	scriptPath := filepath.Join(dir, "scripts", "embed.py")

	// Check Python syntax if python3 is available.
	cmd := exec.Command("python3", "-c",
		"import ast; ast.parse(open('"+scriptPath+"').read())")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("embed.py has syntax errors: %s\n%s", err, string(out))
	}
}

func TestApplyOnErrorWarn(t *testing.T) {
	dir := initKG(t)

	// Create foo so that the link from nonexistent -> foo can succeed
	// (link auto-creates stub for nonexistent source).
	manifest := `
version: 1
actions:
  - action: upsert
    entity: { id: "concept:foo", type: concept, attributes: { name: Foo } }
  - action: link
    from: concept:nonexistent
    rel: related_to
    to: concept:foo
  - action: upsert
    entity: { id: "concept:bar", type: concept, attributes: { name: Bar } }
`
	mPath := filepath.Join(t.TempDir(), "manifest.yaml")
	os.WriteFile(mPath, []byte(manifest), 0o644)

	// With --on-error=warn, all actions should proceed.
	out, err := runCmd(t, "apply", mPath, "--on-error=warn", "--dir="+dir)
	if err != nil {
		t.Fatalf("apply --on-error=warn: %v\noutput: %s", err, out)
	}

	// Foo should exist.
	_, err = runCmd(t, "get", "concept:foo", "--dir="+dir)
	if err != nil {
		t.Error("concept:foo should exist")
	}

	// Bar should also exist (was not skipped).
	_, err = runCmd(t, "get", "concept:bar", "--dir="+dir)
	if err != nil {
		t.Error("concept:bar should exist after --on-error=warn")
	}
}

func TestApplyOnErrorFail(t *testing.T) {
	dir := initKG(t)

	// Manifest with a set on a nonexistent entity — should abort.
	manifest := `
version: 1
actions:
  - action: set
    target: "concept:ghost"
    attributes: { name: Ghost }
  - action: upsert
    entity: { id: "concept:bar", type: concept, attributes: { name: Bar } }
`
	mPath := filepath.Join(t.TempDir(), "manifest.yaml")
	os.WriteFile(mPath, []byte(manifest), 0o644)

	_, err := runCmd(t, "apply", mPath, "--on-error=fail", "--dir="+dir)
	if err == nil {
		t.Fatal("expected error with --on-error=fail on nonexistent target")
	}

	// Bar should NOT exist (apply aborted).
	_, err = runCmd(t, "get", "concept:bar", "--dir="+dir)
	if err == nil {
		t.Error("concept:bar should NOT exist after failed apply")
	}
}

func TestLinkAutoCreatesSource(t *testing.T) {
	dir := initKG(t)

	// Create the target entity.
	_, err := runCmd(t, "create", "concept", "target", "name=Target", "--dir="+dir)
	if err != nil {
		t.Fatalf("create target: %v", err)
	}

	// Link from a nonexistent source — should auto-create stub.
	_, err = runCmd(t, "link", "concept:source", "related_to", "concept:target", "--dir="+dir)
	if err != nil {
		t.Fatalf("link: %v", err)
	}

	// Source should now exist as a stub.
	_, err = runCmd(t, "get", "concept:source", "--dir="+dir)
	if err != nil {
		t.Fatalf("get source after auto-create: %v", err)
	}
}
