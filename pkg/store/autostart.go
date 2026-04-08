package store

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// autoStartDaemon forks a daemon process in the background if one isn't
// already running. Best-effort: failures are silently ignored since the
// caller will fall back to direct index access.
func autoStartDaemon(kgDir string) {
	if os.Getenv("KG_NO_DAEMON") != "" {
		return
	}

	exe, err := os.Executable()
	if err != nil {
		return
	}

	// Guard: only auto-start when running from the real kg binary.
	// During tests, os.Executable() returns the test binary (e.g.,
	// "store.test") which doesn't have a "daemon start" subcommand.
	base := filepath.Base(exe)
	if base != "kg" && base != "kg-ladybug" {
		return
	}

	cmd := exec.Command(exe, "--dir", kgDir, "daemon", "start")
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	_ = cmd.Start() // fire and forget
}
