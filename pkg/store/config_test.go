package store

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadBackendConfig_Default(t *testing.T) {
	dir := t.TempDir() // no config.yaml
	if got := readBackendConfig(dir); got != "ladybug" {
		t.Fatalf("expected ladybug, got %q", got)
	}
}

func TestReadBackendConfig_Ladybug(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "backend: ladybug\n")
	if got := readBackendConfig(dir); got != "ladybug" {
		t.Fatalf("expected ladybug, got %q", got)
	}
}

func TestReadBackendConfig_Auto(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "backend: auto\n")
	if got := readBackendConfig(dir); got != "ladybug" {
		t.Fatalf("expected ladybug, got %q", got)
	}
}

func TestReadBackendConfig_BoltFallsBack(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "backend: bolt\n")
	if got := readBackendConfig(dir); got != "ladybug" {
		t.Fatalf("expected ladybug (bolt no longer supported), got %q", got)
	}
}

func TestReadBackendConfig_Empty(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "backend: \"\"\n")
	if got := readBackendConfig(dir); got != "ladybug" {
		t.Fatalf("expected ladybug, got %q", got)
	}
}

func TestReadBackendConfig_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, ":\n  :\n  - {{invalid\n")
	if got := readBackendConfig(dir); got != "ladybug" {
		t.Fatalf("expected ladybug, got %q", got)
	}
}

func TestActiveBackend(t *testing.T) {
	dir := t.TempDir()
	if got := ActiveBackend(dir); got != "ladybug" {
		t.Fatalf("expected ladybug, got %q", got)
	}
}

func TestOpenIndexFallbackOnDialFailure(t *testing.T) {
	kgDir := t.TempDir()

	// Create the entities/ subdir so the kgDir looks valid.
	if err := os.MkdirAll(filepath.Join(kgDir, "entities"), 0o755); err != nil {
		t.Fatalf("mkdir entities: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(kgDir, ".cache"), 0o755); err != nil {
		t.Fatalf("mkdir .cache: %v", err)
	}

	// Create a stale socket file (a regular file, not a real Unix socket).
	// DaemonAvailable will fail to dial this, causing OpenIndex to fall
	// back to opening the index directly.
	absDir, _ := filepath.Abs(kgDir)
	sockPath := rpcSocketPath(absDir)
	if err := os.WriteFile(sockPath, []byte("not-a-socket"), 0o644); err != nil {
		t.Fatalf("create fake socket: %v", err)
	}

	// Suppress auto-start so we only test the fallback path.
	t.Setenv("KG_NO_DAEMON", "1")

	idx, err := OpenIndex(kgDir)
	if err != nil {
		t.Fatalf("OpenIndex: %v", err)
	}
	defer idx.Close()

	// Verify the index is functional by doing a no-op query.
	if _, err := idx.FindByType("__nonexistent__"); err != nil {
		t.Fatalf("FindByType on fallback index: %v", err)
	}
}
