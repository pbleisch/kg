package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// pidPath returns the path to the daemon PID file for a knowledge graph.
func pidPath(kgDir string) string {
	return filepath.Join(kgDir, ".cache", "daemon.pid")
}

// writePID writes the current process ID to the PID file.
func writePID(kgDir string) error {
	dir := filepath.Join(kgDir, ".cache")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}
	data := []byte(strconv.Itoa(os.Getpid()))
	return os.WriteFile(pidPath(kgDir), data, 0o644)
}

// removePID removes the PID file.
func removePID(kgDir string) error {
	err := os.Remove(pidPath(kgDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// readPID reads the PID from the PID file. Returns 0, nil if the file does
// not exist.
func readPID(kgDir string) (int, error) {
	data, err := os.ReadFile(pidPath(kgDir))
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("invalid pid file: %w", err)
	}
	return pid, nil
}

// isRunning checks whether a process with the given PID is still alive.
func isRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Signal 0 checks for process existence without sending a signal.
	return proc.Signal(syscall.Signal(0)) == nil
}
