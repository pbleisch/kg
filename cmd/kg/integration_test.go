//go:build integration

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// runCmd executes the root Cobra command with the given args and returns
// any captured output (from commands that use cmd.OutOrStdout) and the error.
// It resets the shared cfg struct and every flag between invocations.
func runCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()

	// Reset shared config to defaults.
	cfg.kgDir = ".kg"
	cfg.format = ""
	cfg.quiet = false

	// Cobra keeps flag values on the command tree, and this harness reuses one
	// tree for the whole suite. Without this, a flag one test passes is still
	// set for every test after it.
	resetFlags(rootCmd)

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs(args)

	err := rootCmd.Execute()
	return buf.String(), err
}

// resetFlags restores every flag on a command tree to its default.
func resetFlags(c *cobra.Command) {
	reset := func(f *pflag.Flag) {
		_ = f.Value.Set(f.DefValue)
		f.Changed = false
	}
	c.Flags().VisitAll(reset)
	c.PersistentFlags().VisitAll(reset)
	for _, sub := range c.Commands() {
		resetFlags(sub)
	}
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

	// A graph with no embedder has no derived data to build, and `kg sync`
	// says so rather than exiting 0 having done nothing.
	out, err := runCmd(t, "sync", "--dir="+dir)
	if err == nil {
		t.Fatalf("kg sync with no embedder exited 0: %q", out)
	}
	if !strings.Contains(err.Error()+out, "no embedding command configured") {
		t.Errorf("kg sync failed for the wrong reason: %v", err)
	}

	out, err = runCmd(t, "sync", "--no-embed", "--dir="+dir)
	if err != nil {
		t.Fatalf("kg sync --no-embed failed: %v", err)
	}
	if !strings.Contains(out, "1 entities") || !strings.Contains(out, "embedding skipped") {
		t.Errorf("kg sync --no-embed did not report the graph: %q", out)
	}
}

// TestSyncRebuildsDerivedData is P2's check at the CLI: delete every derived
// artifact, run one command, and the entity files are untouched.
func TestSyncRebuildsDerivedData(t *testing.T) {
	dir := initKG(t)

	script := filepath.Join(t.TempDir(), "embed.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ncat >/dev/null\necho '[0.1, 0.2, 0.3]'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "embedder.yaml"),
		[]byte("command: \""+script+"\"\nfields: [name]\nbatch: false\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := runCmd(t, "create", "person", "alice", "name=Alice", "--dir="+dir); err != nil {
		t.Fatalf("create: %v", err)
	}

	entityPath := filepath.Join(dir, "entities", "person", "alice.yaml")
	before, err := os.ReadFile(entityPath)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := runCmd(t, "sync", "--dir="+dir); err != nil {
		t.Fatalf("kg sync: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".cache", "vectors.bin")); err != nil {
		t.Fatalf("sync built no sidecar: %v", err)
	}

	if err := os.RemoveAll(filepath.Join(dir, ".cache")); err != nil {
		t.Fatal(err)
	}
	if _, err := runCmd(t, "sync", "--dir="+dir); err != nil {
		t.Fatalf("kg sync after deleting the cache: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".cache", "vectors.bin")); err != nil {
		t.Fatalf("sidecar was not rebuilt: %v", err)
	}

	after, err := os.ReadFile(entityPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Errorf("rebuilding derived data changed the entity file:\n%s\n%s", before, after)
	}

	// And the sidecar answers a search.
	out, err := runCmd(t, "similar", "person", "--like", "person:alice", "--field", "name", "--json", "--dir="+dir)
	if err != nil {
		t.Fatalf("kg similar: %v", err)
	}
	if !strings.Contains(out, "person:alice") {
		t.Errorf("kg similar returned nothing useful: %q", out)
	}
}

// `kg daemon` is gone, and nothing may quietly bring it back.
func TestDaemonCommandIsGone(t *testing.T) {
	dir := initKG(t)
	if _, err := runCmd(t, "daemon", "status", "--dir="+dir); err == nil {
		t.Error("kg daemon still exists")
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

	// The command goes to embedder.yaml, never to config.yaml: config.yaml
	// travels with the graph and an interpreter path does not (ADR-0025).
	embData, err := os.ReadFile(filepath.Join(dir, "embedder.yaml"))
	if err != nil {
		t.Fatalf("embedder.yaml not created: %v", err)
	}
	embStr := string(embData)
	if !strings.Contains(embStr, "command:") {
		t.Error("embedder.yaml missing 'command:'")
	}
	for _, line := range strings.Split(embStr, "\n") {
		if !strings.HasPrefix(line, "command:") {
			continue
		}
		for _, tok := range strings.Fields(strings.TrimPrefix(line, "command:")) {
			tok = strings.Trim(tok, `"`)
			if strings.Contains(tok, "/") && !strings.HasPrefix(tok, "/") {
				t.Errorf("embedder.yaml command holds a working-directory-relative path %q: %s", tok, line)
			}
		}
	}

	cfgData, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatalf("reading config.yaml: %v", err)
	}
	if strings.Contains(string(cfgData), "\nembedding:") {
		t.Error("config.yaml gained an embedding block; it travels with the graph")
	}

	// Both the derived cache and the per-checkout embedder are gitignored.
	giData, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("reading .kg/.gitignore: %v", err)
	}
	for _, want := range []string{".cache/", "embedder.yaml"} {
		if !strings.Contains(string(giData), want) {
			t.Errorf(".kg/.gitignore missing %q:\n%s", want, giData)
		}
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

func TestApplyRefusesDanglingLink(t *testing.T) {
	dir := initKG(t)

	// A link from an entity nothing declares is the corruption class this
	// boundary exists to stop: it used to mint a typeless stub and carry on.
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

	out, err := runCmd(t, "apply", mPath, "--on-error=warn", "--dir="+dir)
	if err == nil {
		t.Fatalf("expected the batch to be refused, got: %s", out)
	}
	if !strings.Contains(err.Error(), "concept:nonexistent") {
		t.Errorf("the refusal must name the unresolved reference, got: %v", err)
	}

	// The batch is one transaction: the valid records did not land either.
	if _, err := runCmd(t, "get", "concept:foo", "--dir="+dir); err == nil {
		t.Error("concept:foo should NOT exist after a refused batch")
	}
	if _, err := runCmd(t, "get", "concept:bar", "--dir="+dir); err == nil {
		t.Error("concept:bar should NOT exist after a refused batch")
	}
}

func TestApplyWarnKeepsGoing(t *testing.T) {
	dir := initKG(t)

	// A delete of something that is not there passes structural validation
	// and fails when applied; --on-error=warn carries on past it.
	manifest := `
version: 1
actions:
  - action: upsert
    entity: { id: "concept:foo", type: concept, attributes: { name: Foo } }
  - action: delete
    target: "concept:not_there"
  - action: upsert
    entity: { id: "concept:bar", type: concept, attributes: { name: Bar } }
`
	mPath := filepath.Join(t.TempDir(), "manifest.yaml")
	os.WriteFile(mPath, []byte(manifest), 0o644)

	out, err := runCmd(t, "apply", mPath, "--on-error=warn", "--dir="+dir)
	if err != nil {
		t.Fatalf("apply --on-error=warn: %v\noutput: %s", err, out)
	}
	for _, id := range []string{"concept:foo", "concept:bar"} {
		if _, err := runCmd(t, "get", id, "--dir="+dir); err != nil {
			t.Errorf("%s should exist after --on-error=warn", id)
		}
	}
}

func TestApplyIsIdempotent(t *testing.T) {
	dir := initKG(t)
	manifest := `
version: 1
idempotency_key: fixed.key
actions:
  - action: upsert
    entity: { id: "concept:foo", type: concept, attributes: { name: Foo } }
`
	mPath := filepath.Join(t.TempDir(), "manifest.yaml")
	os.WriteFile(mPath, []byte(manifest), 0o644)

	if _, err := runCmd(t, "apply", mPath, "--dir="+dir); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	before, err := os.ReadFile(filepath.Join(dir, "entities", "concept", "foo.yaml"))
	if err != nil {
		t.Fatalf("reading entity: %v", err)
	}
	if _, err := runCmd(t, "apply", mPath, "--dir="+dir); err != nil {
		t.Fatalf("second apply: %v", err)
	}
	after, err := os.ReadFile(filepath.Join(dir, "entities", "concept", "foo.yaml"))
	if err != nil {
		t.Fatalf("reading entity: %v", err)
	}
	if string(before) != string(after) {
		t.Error("applying the same batch twice changed the graph")
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

func TestLinkRefusesMissingSource(t *testing.T) {
	dir := initKG(t)

	// Create the target entity.
	if _, err := runCmd(t, "create", "concept", "target", "name=Target", "--dir="+dir); err != nil {
		t.Fatalf("create target: %v", err)
	}

	// Linking from an entity nobody submitted used to auto-create a typeless
	// stub.  It is refused now, and no stub is left behind.
	if _, err := runCmd(t, "link", "concept:source", "related_to", "concept:target", "--dir="+dir); err == nil {
		t.Fatal("expected link from a nonexistent source to be refused")
	}
	if _, err := runCmd(t, "get", "concept:source", "--dir="+dir); err == nil {
		t.Fatal("a stub entity was created for the missing source")
	}
}
