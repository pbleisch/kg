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
