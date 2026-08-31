package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// scratchCommand writes an executable that satisfies validateEmbedder and
// returns its absolute path.
func scratchCommand(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "embed.sh")
	if err := os.WriteFile(p, []byte("#!/bin/sh\ncat >/dev/null\necho '[0.1]'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestResolveEmbedder_NoneConfigured(t *testing.T) {
	dir := t.TempDir()
	_, err := ResolveEmbedder(dir)
	var missing *ErrNoEmbedder
	if !errors.As(err, &missing) {
		t.Fatalf("got %v, want *ErrNoEmbedder", err)
	}
	if !strings.Contains(err.Error(), "KG_EMBED_COMMAND") || !strings.Contains(err.Error(), EmbedderName) {
		t.Errorf("the error does not say how to fix it: %v", err)
	}
}

func TestResolveEmbedder_FromFile(t *testing.T) {
	dir := t.TempDir()
	cmd := scratchCommand(t)
	writeEmbedder(t, dir, cmd, []string{"name", "description"}, true)

	emb, err := ResolveEmbedder(dir)
	if err != nil {
		t.Fatalf("ResolveEmbedder: %v", err)
	}
	if emb.Command != cmd {
		t.Errorf("Command = %q, want %q", emb.Command, cmd)
	}
	if len(emb.Fields) != 2 || emb.Fields[0] != "name" {
		t.Errorf("Fields = %v", emb.Fields)
	}
	if !emb.Batch {
		t.Error("Batch = false, want true")
	}
	if emb.Source != EmbedderPath(dir) {
		t.Errorf("Source = %q, want the file it came from", emb.Source)
	}
}

// TestResolveEmbedder_EnvWins is how a pipeline in the code repository points
// kg at its own virtualenv without writing into the data repository.
func TestResolveEmbedder_EnvWins(t *testing.T) {
	dir := t.TempDir()
	fileCmd := scratchCommand(t)
	envCmd := scratchCommand(t)
	writeEmbedder(t, dir, fileCmd, []string{"name"}, false)

	t.Setenv("KG_EMBED_COMMAND", envCmd)
	t.Setenv("KG_EMBED_FIELDS", "title, body")
	t.Setenv("KG_EMBED_BATCH", "true")

	emb, err := ResolveEmbedder(dir)
	if err != nil {
		t.Fatalf("ResolveEmbedder: %v", err)
	}
	if emb.Command != envCmd {
		t.Errorf("Command = %q, want the environment's %q", emb.Command, envCmd)
	}
	if len(emb.Fields) != 2 || emb.Fields[1] != "body" {
		t.Errorf("Fields = %v, want [title body]", emb.Fields)
	}
	if !emb.Batch {
		t.Error("KG_EMBED_BATCH was not honoured")
	}
	if emb.Source != "KG_EMBED_COMMAND" {
		t.Errorf("Source = %q", emb.Source)
	}
}

// TestResolveEmbedder_RefusesCWDRelative is the bug this file exists to
// prevent: a command that resolves against whatever directory kg is run from.
func TestResolveEmbedder_RefusesCWDRelative(t *testing.T) {
	for _, cmd := range []string{
		".venv/bin/python .kg/scripts/embed.py",
		"./embed.sh",
		"../tools/embed",
	} {
		dir := t.TempDir()
		writeEmbedder(t, dir, cmd, []string{"name"}, false)
		_, err := ResolveEmbedder(dir)
		if err == nil {
			t.Errorf("ResolveEmbedder accepted the working-directory-relative command %q", cmd)
			continue
		}
		if !strings.Contains(err.Error(), "working directory") && !strings.Contains(err.Error(), "absolute") {
			t.Errorf("%q: error does not explain the problem: %v", cmd, err)
		}
	}
}

// A relative *argument* is exactly as CWD-dependent as a relative program.
func TestResolveEmbedder_RefusesCWDRelativeArgument(t *testing.T) {
	dir := t.TempDir()
	writeEmbedder(t, dir, "/bin/sh .kg/scripts/embed.py", []string{"name"}, false)
	_, err := ResolveEmbedder(dir)
	if err == nil {
		t.Fatal("ResolveEmbedder accepted a relative script argument")
	}
	if !strings.Contains(err.Error(), "relative") {
		t.Errorf("error does not name the problem: %v", err)
	}
}

func TestResolveEmbedder_AcceptsProgramOnPath(t *testing.T) {
	dir := t.TempDir()
	writeEmbedder(t, dir, "sh -c true", []string{"name"}, false)
	if _, err := ResolveEmbedder(dir); err != nil {
		t.Fatalf("ResolveEmbedder refused a bare program on PATH: %v", err)
	}
}

func TestResolveEmbedder_RefusesMissingProgram(t *testing.T) {
	dir := t.TempDir()
	writeEmbedder(t, dir, filepath.Join(t.TempDir(), "not-there"), []string{"name"}, false)
	if _, err := ResolveEmbedder(dir); err == nil {
		t.Fatal("ResolveEmbedder accepted a command that is not there")
	}
}

func TestResolveEmbedder_RefusesNoFields(t *testing.T) {
	dir := t.TempDir()
	writeEmbedder(t, dir, scratchCommand(t), nil, false)
	if _, err := ResolveEmbedder(dir); err == nil {
		t.Fatal("ResolveEmbedder accepted an embedder with no fields to embed")
	}
}

// TestResolveEmbedder_RefusesGraphEmbedding is the migration guard: a corpus
// that still configures embedding in the file that travels with the data gets
// told so, rather than quietly embedding nothing.
func TestResolveEmbedder_RefusesGraphEmbedding(t *testing.T) {
	dir := t.TempDir()
	config := `backend: ladybug
embedding:
  command: ".venv/bin/python .kg/scripts/embed.py"
  fields: [name, description]
  batch: true
`
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ResolveEmbedder(dir)
	if err == nil {
		t.Fatal("ResolveEmbedder ignored an embedding block in config.yaml")
	}
	msg := err.Error()
	for _, want := range []string{"config.yaml", EmbedderName, "KG_EMBED_COMMAND", "ADR-0025"} {
		if !strings.Contains(msg, want) {
			t.Errorf("migration message does not mention %q:\n%s", want, msg)
		}
	}

	// And it stays refused even when a valid embedder is configured: leaving
	// the dead block in place is exactly the state that reads as configured
	// and does nothing.
	writeEmbedder(t, dir, scratchCommand(t), []string{"name"}, false)
	if _, err := ResolveEmbedder(dir); err == nil {
		t.Error("the stale block was tolerated once a real embedder existed")
	}
}

func TestResolveEmbedder_PlainConfigIsFine(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("# nothing kg reads\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeEmbedder(t, dir, scratchCommand(t), []string{"name"}, false)
	if _, err := ResolveEmbedder(dir); err != nil {
		t.Fatalf("a config.yaml with no embedding block should be inert: %v", err)
	}
}

func TestEmbedderDigestChangesWithCommand(t *testing.T) {
	a := (&Embedder{Command: "a"}).Digest()
	b := (&Embedder{Command: "b"}).Digest()
	if a == b {
		t.Error("two different commands share a digest")
	}
	if a != (&Embedder{Command: "a"}).Digest() {
		t.Error("the digest is not stable")
	}
	if strings.Contains(a, "a") && !strings.HasPrefix(a, "sha256:") {
		t.Errorf("digest %q looks like it leaks the command", a)
	}
}

// TestEmbedderDigestCoversScript: editing the scaffolded script is what
// changing the model usually means locally, and it must invalidate the sidecar.
func TestEmbedderDigestCoversScript(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(t.TempDir(), "embed.py")
	if err := os.WriteFile(script, []byte("# model A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeEmbedder(t, dir, "sh "+script, []string{"name"}, false)

	emb, err := ResolveEmbedder(dir)
	if err != nil {
		t.Fatal(err)
	}
	first := emb.Digest()

	if err := os.WriteFile(script, []byte("# model B\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	emb, err = ResolveEmbedder(dir)
	if err != nil {
		t.Fatal(err)
	}
	if emb.Digest() == first {
		t.Error("editing the embedding script left the digest alone; a model swap would go unnoticed")
	}
}
