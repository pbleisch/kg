//go:build mage

package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"

	"github.com/magefile/mage/mg"
	"github.com/magefile/mage/sh"
)

const (
	binary      = "kg"
	binDir      = "bin"
	cmdPkg      = "./cmd/kg/"
	lbugVersion = "v0.13.1"
	lbugLibDir  = "lib-ladybug"
)

var (
	goOS   = runtime.GOOS
	goArch = runtime.GOARCH
)

// Default target when running mage without arguments.
var Default = Build

// Build compiles the kg binary to bin/kg.
func Build() error {
	return sh.RunV("go", "build", "-o", filepath.Join(binDir, binary), cmdPkg)
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
	fmt.Printf("Installed to %s\n", dst)
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

// Lint runs go vet.
func Lint() error {
	return sh.RunV("go", "vet", "./...")
}

// Install installs kg to GOPATH/bin.
func Install() error {
	return sh.RunV("go", "install", cmdPkg)
}

// Clean removes build artifacts.
func Clean() error {
	return os.RemoveAll(binDir)
}

// Check runs lint then test (CI convenience).
func Check() error {
	if err := Lint(); err != nil {
		return err
	}
	return Test()
}

// Cover runs tests with coverage and generates coverage.html.
func Cover() error {
	if err := sh.RunV("go", "test", "./...", "-count=1", "-coverprofile=coverage.out"); err != nil {
		return err
	}
	return sh.RunV("go", "tool", "cover", "-html=coverage.out", "-o", "coverage.html")
}

// ─── LadybugDB targets ─────────────────────────────────────────────────────

// Ladybug groups build targets that require the LadybugDB native library.
type Ladybug mg.Namespace

// Download downloads the LadybugDB native library to lib-ladybug/.
func (Ladybug) Download() error {
	return ensureLbugLib()
}

// Build compiles kg-ladybug with CGO and the native library.
func (Ladybug) Build() error {
	if err := ensureLbugLib(); err != nil {
		return err
	}
	env := lbugCGOEnv()
	if err := sh.RunWithV(env, "go", "build", "-tags", "ladybug",
		"-ldflags", "-s -w -X main.version=dev",
		"-o", filepath.Join(binDir, binary+"-ladybug"), cmdPkg,
	); err != nil {
		return err
	}
	dstLib := filepath.Join(binDir, lbugLibDir)
	if err := os.MkdirAll(dstLib, 0o755); err != nil {
		return err
	}
	libName := lbugLibName()
	if err := copyFile(filepath.Join(lbugLibDir, libName), filepath.Join(dstLib, libName)); err != nil {
		return err
	}
	fmt.Printf("Built %s (lib → %s/%s)\n",
		filepath.Join(binDir, binary+"-ladybug"), dstLib, libName)
	return nil
}

// Test runs tests with the ladybug backend.
func (Ladybug) Test() error {
	if err := ensureLbugLib(); err != nil {
		return err
	}
	env := mergeMaps(lbugTestEnv(), lbugCGOEnv())
	return sh.RunWithV(env, "go", "test", "-tags", "ladybug", "./...", "-count=1")
}

// Local builds kg-ladybug and copies it with the native lib to .kg/bin/.
func (Ladybug) Local() error {
	lb := Ladybug{}
	if err := lb.Build(); err != nil {
		return err
	}
	dir := filepath.Join(".kg", "bin")
	libDst := filepath.Join(dir, lbugLibDir)
	if err := os.MkdirAll(libDst, 0o755); err != nil {
		return err
	}
	bin := filepath.Join(dir, "kg-ladybug-"+goOS)
	if err := copyFile(filepath.Join(binDir, binary+"-ladybug"), bin); err != nil {
		return err
	}
	libName := lbugLibName()
	if err := copyFile(filepath.Join(lbugLibDir, libName), filepath.Join(libDst, libName)); err != nil {
		return err
	}
	fmt.Printf("Installed to %s\n", bin)
	return nil
}

// Install builds and installs kg-ladybug and the native lib to GOPATH/bin.
func (Ladybug) Install() error {
	lb := Ladybug{}
	if err := lb.Build(); err != nil {
		return err
	}
	gopath, err := sh.Output("go", "env", "GOPATH")
	if err != nil {
		return err
	}
	gopathBin := filepath.Join(gopath, "bin")
	libDst := filepath.Join(gopathBin, lbugLibDir)
	if err := os.MkdirAll(libDst, 0o755); err != nil {
		return err
	}
	if err := copyFile(filepath.Join(binDir, binary+"-ladybug"), filepath.Join(gopathBin, "kg-ladybug")); err != nil {
		return err
	}
	libName := lbugLibName()
	if err := copyFile(filepath.Join(lbugLibDir, libName), filepath.Join(libDst, libName)); err != nil {
		return err
	}
	fmt.Printf("Installed kg-ladybug to %s\n", gopathBin)
	fmt.Printf("Installed lib to %s/%s\n", libDst, libName)
	return nil
}

// Check runs lint then test with the ladybug backend.
func (Ladybug) Check() error {
	if err := Lint(); err != nil {
		return err
	}
	lb := Ladybug{}
	return lb.Test()
}

// ─── Ladybug helpers ────────────────────────────────────────────────────────

func lbugLibName() string {
	if goOS == "darwin" {
		return "liblbug.dylib"
	}
	if goOS == "windows" {
		return "lbug_shared.dll"
	}
	return "liblbug.so"
}

func lbugAssetName() string {
	switch {
	case goOS == "darwin":
		return "liblbug-osx-universal.tar.gz"
	case goOS == "linux" && goArch == "arm64":
		return "liblbug-linux-aarch64.tar.gz"
	default:
		return "liblbug-linux-x86_64.tar.gz"
	}
}

func lbugRpath() string {
	if goOS == "darwin" {
		return "@loader_path/" + lbugLibDir
	}
	return "$ORIGIN/" + lbugLibDir
}

func lbugCGOEnv() map[string]string {
	cwd, _ := os.Getwd()
	return map[string]string{
		"CGO_ENABLED": "1",
		"CGO_LDFLAGS": fmt.Sprintf("-L%s -Wl,-rpath,%s",
			filepath.Join(cwd, lbugLibDir), lbugRpath()),
	}
}

func lbugTestEnv() map[string]string {
	cwd, _ := os.Getwd()
	libPath := filepath.Join(cwd, lbugLibDir)
	if goOS == "darwin" {
		return map[string]string{"DYLD_LIBRARY_PATH": libPath}
	}
	return map[string]string{"LD_LIBRARY_PATH": libPath}
}

func ensureLbugLib() error {
	libPath := filepath.Join(lbugLibDir, lbugLibName())
	if _, err := os.Stat(libPath); err == nil {
		return nil
	}

	asset := lbugAssetName()
	url := fmt.Sprintf(
		"https://github.com/LadybugDB/ladybug/releases/download/%s/%s",
		lbugVersion, asset)
	fmt.Printf("Downloading LadybugDB %s (%s)...\n", lbugVersion, asset)

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return fmt.Errorf("gzip read failed: %w", err)
	}
	defer gz.Close()

	if err := os.MkdirAll(lbugLibDir, 0o755); err != nil {
		return err
	}

	libName := lbugLibName()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar read failed: %w", err)
		}
		if filepath.Base(hdr.Name) == libName {
			dst := filepath.Join(lbugLibDir, libName)
			f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			f.Close()
			fmt.Printf("  -> %s\n", dst)
			return nil
		}
	}
	return fmt.Errorf("%s not found in archive", libName)
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

func mergeMaps(maps ...map[string]string) map[string]string {
	result := make(map[string]string)
	for _, m := range maps {
		for k, v := range m {
			result[k] = v
		}
	}
	return result
}
