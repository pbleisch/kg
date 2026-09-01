//go:build integration

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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

func TestSyncInit(t *testing.T) {
	dir := initKG(t)

	out, err := runCmd(t, "sync", "--init", "--dir="+dir)
	if err != nil {
		t.Fatalf("sync --init: %v", err)
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
	_, err = runCmd(t, "sync", "--init", "--dir="+dir)
	if err == nil {
		t.Fatal("expected error on second sync --init")
	}
}

func TestSyncInitScriptSyntax(t *testing.T) {
	dir := initKG(t)
	runCmd(t, "sync", "--init", "--dir="+dir)

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

// seedFile writes an entity file directly, without the write boundary,
// because every violation these tests seed is one the boundary refuses.
func seedFile(t *testing.T, kgDir, typeName, file, body string) string {
	t.Helper()
	dir := filepath.Join(kgDir, "entities", typeName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, file)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// The done-condition of increment 4, one violation class per subtest: a
// structural one fails the load naming the offending records, and a dangling
// target is reported and exits 0 (ADR-0028).
func TestValidateSeededViolations(t *testing.T) {
	t.Run("two files claiming one identity", func(t *testing.T) {
		dir := initKG(t)
		seedFile(t, dir, "source", "m%C3%A4yra.yaml", "id: source:mäyra\ntype: source\n")
		seedFile(t, dir, "source", "mäyra.yaml", "id: source:mäyra\ntype: source\n")

		out, err := runCmd(t, "validate", "--dir="+dir)
		if err == nil {
			t.Fatalf("kg validate exited 0 on a duplicate identity: %q", out)
		}
		if !strings.Contains(out, "m%C3%A4yra.yaml") || !strings.Contains(out, "mäyra.yaml") {
			t.Errorf("the refusal did not name both files:\n%s", out)
		}
	})

	t.Run("a file in the wrong place", func(t *testing.T) {
		dir := initKG(t)
		seedFile(t, dir, "person", "alice.yaml", "id: person:alicia\ntype: person\n")

		out, err := runCmd(t, "validate", "--dir="+dir)
		if err == nil {
			t.Fatalf("kg validate exited 0 on an id/path disagreement: %q", out)
		}
		if !strings.Contains(out, "person:alicia") || !strings.Contains(out, "person:alice") {
			t.Errorf("the refusal did not name both identities:\n%s", out)
		}
	})

	t.Run("an illegal attribute name", func(t *testing.T) {
		dir := initKG(t)
		seedFile(t, dir, "person", "alice.yaml",
			"id: person:alice\ntype: person\nattributes:\n  --dir: .kg\n")

		out, err := runCmd(t, "validate", "--dir="+dir)
		if err == nil {
			t.Fatalf("kg validate exited 0 on an illegal attribute name: %q", out)
		}
		if !strings.Contains(out, "--dir") {
			t.Errorf("the refusal did not name the attribute:\n%s", out)
		}
	})

	t.Run("a relation pointing at nothing is reported", func(t *testing.T) {
		dir := initKG(t)
		seedFile(t, dir, "person", "alice.yaml",
			"id: person:alice\ntype: person\nrelations:\n  knows:\n    - person:nobody\n")

		out, err := runCmd(t, "validate", "--dir="+dir)
		if err != nil {
			t.Fatalf("a dangling target refused the load: %v\n%s", err, out)
		}
		if !strings.Contains(out, "dangling_relation") || !strings.Contains(out, "person:nobody") {
			t.Errorf("the dangling target was not reported:\n%s", out)
		}
	})
}

// `kg validate --fix` repairs the classes a rule can repair and moves no file.
func TestValidateFixIsPathAuthoritative(t *testing.T) {
	dir := initKG(t)
	occupant := seedFile(t, dir, "person", "alice.yaml",
		"id: person:alice\ntype: person\nattributes:\n  name: The one already there\n")
	stray := seedFile(t, dir, "people", "alice.yaml",
		"id: person:alice\ntype: person\nattributes:\n  name: The stray\n  --dir: .kg\n")

	if _, err := runCmd(t, "validate", "--dir="+dir); err == nil {
		t.Fatal("kg validate exited 0 on a corpus with two violations")
	}
	out, err := runCmd(t, "validate", "--fix", "--dir="+dir)
	if err != nil {
		t.Fatalf("kg validate --fix: %v\n%s", err, out)
	}
	if _, err := os.Stat(stray); err != nil {
		t.Errorf("--fix moved or removed a file: %v", err)
	}
	if _, err := os.Stat(occupant); err != nil {
		t.Errorf("--fix clobbered the occupant: %v", err)
	}
	if _, err := runCmd(t, "validate", "--dir="+dir); err != nil {
		t.Errorf("the graph still does not load after --fix: %v", err)
	}

	got, err := os.ReadFile(occupant)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "The one already there") {
		t.Errorf("person:alice was overwritten by the stray:\n%s", got)
	}
}

// A graph full of warnings still loads.
func TestValidateWarningsExitZero(t *testing.T) {
	dir := initKG(t)
	for _, name := range []string{"alice", "bob"} {
		seedFile(t, dir, "person", name+".yaml",
			"id: person:"+name+"\ntype: person\nattributes:\n  whatever: x\nrelations:\n  knows:\n    - person:nobody\n")
	}
	out, err := runCmd(t, "validate", "--dir="+dir)
	if err != nil {
		t.Fatalf("a graph of warnings refused: %v\n%s", err, out)
	}
	if !strings.Contains(out, "0 structural error(s)") {
		t.Errorf("expected no structural errors:\n%s", out)
	}
}

// Resolve refuses a partial index rather than answering from one: the pass it
// replaced was permanently one document behind.
func TestResolveRefusesAnIncompleteIndex(t *testing.T) {
	dir := initKG(t)
	seedFile(t, dir, "person", "alice.yaml",
		"id: person:alice\ntype: person\nattributes:\n  name: Alice\n")

	out, err := runCmd(t, "resolve", "--dir="+dir)
	if err == nil {
		t.Fatalf("kg resolve answered with no sidecar at all: %q", out)
	}
}

// ---------------------------------------------------------------------------
// The verbs that went, and the surfaces that must not name them again
// ---------------------------------------------------------------------------

// Each of these was deleted for a reason recorded in the commit that removed
// it, and each is the kind of thing a later change re-adds by accident. This
// is the TestDaemonCommandIsGone shape, one case per verb.
func TestRemovedCommandsAreGone(t *testing.T) {
	dir := initKG(t)
	for _, tc := range []struct {
		name string
		args []string
		why  string
	}{
		{"import", []string{"import", "/dev/null"},
			"destroyed relations absent from the file and never entered batch.Apply; `kg apply` with an upsert action replaces it"},
		{"morph", []string{"morph", "--", "friends", "=", "->", "knows"},
			"defined patterns `kg path` could never execute"},
		{"dedup", []string{"dedup", "person", "--fields", "name"},
			"answered from a partial index where `kg resolve` refuses"},
		{"watch", []string{"watch", "--json"},
			"daemon-era (ADR-0023); internal/watch went with it"},
		{"embed", []string{"embed"},
			"folded into `kg sync`, which is the name gdkb and the docs use"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := append(append([]string{}, tc.args...), "--dir="+dir)
			if _, err := runCmd(t, args...); err == nil {
				t.Errorf("kg %s still exists; it was removed because it %s", tc.name, tc.why)
			}
		})
	}
}

// The generated SKILL.md is a contract handed to an agent. A command named in
// it that kg does not have is an instruction to run something that fails.
func TestGeneratedSkillNamesOnlyRealCommands(t *testing.T) {
	dir := initKG(t)
	out, err := runCmd(t, "agent", "skill", "--dir="+dir)
	if err != nil {
		t.Fatalf("agent skill: %v", err)
	}
	have := map[string]bool{}
	for _, c := range rootCmd.Commands() {
		have[c.Name()] = true
	}
	// Only the fenced blocks: the prose says things like "kg has one merge
	// rule", and a verb is what an agent is told to run, not what a sentence
	// happens to start with.
	inFence := false
	verb := regexp.MustCompile(`^\s*(?:\$ )?kg ([a-z][a-z-]*)`)
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		if !inFence {
			continue
		}
		if m := verb.FindStringSubmatch(line); m != nil && !have[m[1]] {
			t.Errorf("SKILL.md names `kg %s`, which no longer exists", m[1])
		}
	}
	for _, gone := range []string{"import", "morph", "dedup", "watch", "embed"} {
		if strings.Contains(out, "kg "+gone) {
			t.Errorf("SKILL.md still names the removed `kg %s`", gone)
		}
	}
}

// `kg sync` absorbed `kg embed`, and gdkb calls it in three places with no
// flags. Those call sites must keep working verbatim.
func TestSyncCoversWhatEmbedDid(t *testing.T) {
	dir := initKG(t)
	for _, args := range [][]string{
		{"sync", "--show"},     // was `embed --show`
		{"sync", "--no-embed"}, // gdkb's shape, no embedder configured
		{"sync", "--dry-run", "--no-embed"},
		{"sync", "person:alice", "--no-embed"}, // was `embed <entity-id>`
	} {
		full := append(append([]string{}, args...), "--dir="+dir)
		if _, err := runCmd(t, full...); err != nil {
			t.Errorf("kg %v: %v", args, err)
		}
	}
}

// A merge is a write, and every write carries an envelope, a key and a
// journal. `kg merge` used to be the one that did not.
func TestMergeGoesThroughTheWriteBoundary(t *testing.T) {
	dir := initKG(t)
	for _, a := range [][]string{
		{"create", "person", "al", "name=Al"},
		{"create", "person", "alice", "name=Alice"},
		{"create", "person", "carol", "name=Carol"},
	} {
		if _, err := runCmd(t, append(a, "--dir="+dir)...); err != nil {
			t.Fatalf("setup %v: %v", a, err)
		}
	}
	if _, err := runCmd(t, "link", "person:carol", "knows", "person:al", "--dir="+dir); err != nil {
		t.Fatalf("link: %v", err)
	}
	if _, err := runCmd(t, "merge", "person:al", "person:alice", "--dir="+dir); err != nil {
		t.Fatalf("merge: %v", err)
	}

	// The ledger is the evidence: a merge that did not enter batch.Apply
	// leaves no entry behind.
	entries, err := os.ReadDir(filepath.Join(dir, ".cache", "apply"))
	if err != nil {
		t.Fatalf("reading ledger: %v", err)
	}
	found := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "merge:") {
			found = true
		}
	}
	if !found {
		t.Error("kg merge wrote no ledger entry; it is not going through the write boundary")
	}

	// The inbound link was rewritten and the source is gone.
	if _, err := runCmd(t, "get", "person:al", "--dir="+dir); err == nil {
		t.Error("source entity survived the merge")
	}
	carol, err := os.ReadFile(filepath.Join(dir, "entities", "person", "carol.yaml"))
	if err != nil {
		t.Fatalf("reading carol: %v", err)
	}
	if !strings.Contains(string(carol), "person:alice") {
		t.Errorf("inbound link not rewritten to the target: %s", carol)
	}
	if strings.Contains(string(carol), "person:al\n") {
		t.Errorf("inbound link still points at the merged-away source: %s", carol)
	}
}

// --cross-type still guards an interactive typo, and the guard now lives at
// the boundary rather than in cmd/.
func TestMergeRefusesCrossTypeWithoutTheFlag(t *testing.T) {
	dir := initKG(t)
	for _, a := range [][]string{
		{"create", "person", "carol", "name=Carol"},
		{"create", "org", "acme", "name=Acme"},
	} {
		if _, err := runCmd(t, append(a, "--dir="+dir)...); err != nil {
			t.Fatalf("setup %v: %v", a, err)
		}
	}
	if _, err := runCmd(t, "merge", "person:carol", "org:acme", "--dir="+dir); err == nil {
		t.Error("merged across types without --cross-type")
	}
	if _, err := runCmd(t, "merge", "person:carol", "org:acme", "--cross-type", "--dir="+dir); err != nil {
		t.Errorf("--cross-type did not allow the merge: %v", err)
	}
}
