package store

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
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

// EnsureDaemon starts the daemon if it is not already running and blocks
// until the RPC socket accepts connections. Returns an error if the daemon
// does not become available within 10 seconds.
func EnsureDaemon(kgDir string) error {
	absDir, err := filepath.Abs(kgDir)
	if err != nil {
		return fmt.Errorf("resolve kg dir: %w", err)
	}
	sockPath := rpcSocketPath(absDir)

	if DaemonAvailable(sockPath) {
		return nil
	}

	if os.Getenv("KG_NO_DAEMON") != "" {
		return fmt.Errorf("daemon required but KG_NO_DAEMON is set")
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot determine executable path: %w", err)
	}

	cmd := exec.Command(exe, "--dir", absDir, "daemon", "start")
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting daemon: %w", err)
	}

	const timeout = 10 * time.Second
	const poll = 50 * time.Millisecond
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		time.Sleep(poll)
		if DaemonAvailable(sockPath) {
			return nil
		}
	}
	return fmt.Errorf("daemon did not become available within %s", timeout)
}
