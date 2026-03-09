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
	if got := readBackendConfig(dir); got != "auto" {
		t.Fatalf("expected auto, got %q", got)
	}
}

func TestReadBackendConfig_Bolt(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "backend: bolt\n")
	if got := readBackendConfig(dir); got != "bolt" {
		t.Fatalf("expected bolt, got %q", got)
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
	if got := readBackendConfig(dir); got != "auto" {
		t.Fatalf("expected auto, got %q", got)
	}
}

func TestReadBackendConfig_Empty(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "backend: \"\"\n")
	if got := readBackendConfig(dir); got != "auto" {
		t.Fatalf("expected auto, got %q", got)
	}
}

func TestReadBackendConfig_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, ":\n  :\n  - {{invalid\n")
	if got := readBackendConfig(dir); got != "auto" {
		t.Fatalf("expected auto, got %q", got)
	}
}

func TestActiveBackend_Default(t *testing.T) {
	dir := t.TempDir()
	got := ActiveBackend(dir)
	want := "bolt"
	if ladybugAvailable {
		want = "ladybug"
	}
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestActiveBackend_ForceBolt(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "backend: bolt\n")
	if got := ActiveBackend(dir); got != "bolt" {
		t.Fatalf("expected bolt, got %q", got)
	}
}

func TestActiveBackend_ForceLadybug(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "backend: ladybug\n")
	want := "ladybug (unavailable)"
	if ladybugAvailable {
		want = "ladybug"
	}
	if got := ActiveBackend(dir); got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestOpenIndex_BoltConfig(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "backend: bolt\n")

	idx, err := OpenIndex(dir)
	if err != nil {
		t.Fatalf("OpenIndex failed: %v", err)
	}
	defer idx.Close()

	if _, ok := idx.(*boltIndex); !ok {
		t.Fatalf("expected *boltIndex, got %T", idx)
	}
}
