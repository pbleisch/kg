//go:build mage

package main

import (
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/magefile/mage/mg"
	"github.com/magefile/mage/sh"
)

const (
	binary = "kg"
	binDir = "bin"
	cmdPkg = "./cmd/kg/"
)

var goOS = runtime.GOOS

// Default target when running mage without arguments.
var Default = Build

// Build compiles the kg binary to bin/kg.
//
// No CGO, no native library, no download step: kg is pure Go since the
// LadybugDB index was removed (ADR-0027).
func Build() error {
	return sh.RunV("go", "build",
		"-ldflags", "-s -w -X main.version=dev",
		"-o", filepath.Join(binDir, binary), cmdPkg)
}

// Local builds and copies the binary to .kg/bin/kg-<os> for local testing.
func Local() error {
	mg.Deps(Build)
	dir := filepath.Join(".kg", "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	dst := filepath.Join(dir, "kg-"+goOS)
	if err := copyFile(filepath.Join(binDir, binary), dst); err != nil {
		return err
	}
	os.Stdout.WriteString("Installed to " + dst + "\n")
	return nil
}

// Test runs all tests.
func Test() error {
	return sh.RunV("go", "test", "./...", "-count=1")
}

// TestRace runs tests with the race detector.
func TestRace() error {
	return sh.RunV("go", "test", "-race", "./...", "-count=1")
}

// TestIntegration runs the integration-tagged tests.
func TestIntegration() error {
	return sh.RunV("go", "test", "-tags=integration", "./...", "-count=1")
}

// Lint runs go vet and checks formatting.
func Lint() error {
	if err := sh.RunV("go", "vet", "./..."); err != nil {
		return err
	}
	out, err := sh.Output("gofmt", "-l", ".")
	if err != nil {
		return err
	}
	if out != "" {
		os.Stderr.WriteString("gofmt needs to run on:\n" + out + "\n")
		return sh.RunV("false")
	}
	return nil
}

// Install installs kg to GOPATH/bin.
func Install() error {
	return sh.RunV("go", "install", cmdPkg)
}

// Clean removes build artifacts.
func Clean() error {
	return os.RemoveAll(binDir)
}

// Check runs lint, then tests, then the integration tests (CI convenience).
func Check() error {
	if err := Lint(); err != nil {
		return err
	}
	if err := Test(); err != nil {
		return err
	}
	return TestIntegration()
}

// Cover runs tests with coverage and generates coverage.html.
func Cover() error {
	if err := sh.RunV("go", "test", "./...", "-count=1", "-coverprofile=coverage.out"); err != nil {
		return err
	}
	return sh.RunV("go", "tool", "cover", "-html=coverage.out", "-o", "coverage.html")
}

// ─── Generic helpers ────────────────────────────────────────────────────────

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
