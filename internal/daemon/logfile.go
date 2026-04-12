package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	maxLogSize   = 5 * 1024 * 1024 // 5MB
	logFileName  = "daemon.log"
	logBackup    = "daemon.log.1"
	logSentinel  = "daemon.logfile"
)

func logFilePath(kgDir string) string {
	return filepath.Join(kgDir, ".cache", logFileName)
}

func logSentinelPath(kgDir string) string {
	return filepath.Join(kgDir, ".cache", logSentinel)
}

// openLogFile opens the daemon log file for writing, rotating it first if it
// exceeds maxLogSize.
func openLogFile(kgDir string) (*os.File, error) {
	p := logFilePath(kgDir)
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create cache dir: %w", err)
	}

	// Rotate if existing log is too large.
	if info, err := os.Stat(p); err == nil && info.Size() > maxLogSize {
		backup := filepath.Join(dir, logBackup)
		os.Rename(p, backup) // best-effort; overwrite previous backup
	}

	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}
	return f, nil
}

// writeLogSentinel records the absolute path of the active log file so
// external tools can locate it.
func writeLogSentinel(kgDir string, logPath string) error {
	return os.WriteFile(logSentinelPath(kgDir), []byte(logPath), 0o644)
}

// removeLogSentinel deletes the log sentinel file.
func removeLogSentinel(kgDir string) error {
	err := os.Remove(logSentinelPath(kgDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// LogFilePath reads the sentinel file and returns the path to the active
// daemon log. Returns an error if the sentinel does not exist (daemon not
// running or no log).
func LogFilePath(kgDir string) (string, error) {
	data, err := os.ReadFile(logSentinelPath(kgDir))
	if err != nil {
		return "", fmt.Errorf("read log sentinel: %w", err)
	}
	p := strings.TrimSpace(string(data))
	if p == "" {
		return "", fmt.Errorf("log sentinel is empty")
	}
	return p, nil
}
