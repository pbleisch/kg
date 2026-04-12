package daemon

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pbleisch/kg/pkg/store"
)

// setupTestKG creates a minimal .kg directory with entities/ and .cache/ dirs.
func setupTestKG(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "entities"), 0o755); err != nil {
		t.Fatalf("mkdir entities: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".cache"), 0o755); err != nil {
		t.Fatalf("mkdir .cache: %v", err)
	}
	return dir
}

func TestStartStop(t *testing.T) {
	kgDir := setupTestKG(t)

	d, err := Start(kgDir)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// PID file should exist.
	pid, err := readPID(kgDir)
	if err != nil {
		t.Fatalf("readPID: %v", err)
	}
	if pid != os.Getpid() {
		t.Errorf("pid = %d, want %d", pid, os.Getpid())
	}

	// Socket should be reachable.
	conn, err := net.Dial("unix", SocketPathFor(kgDir))
	if err != nil {
		t.Fatalf("dial socket: %v", err)
	}
	conn.Write([]byte("status"))
	buf := make([]byte, 64)
	n, _ := conn.Read(buf)
	if got := string(buf[:n]); got != "running" {
		t.Errorf("status = %q, want running", got)
	}
	conn.Close()

	if err := d.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Socket file should be removed.
	if _, err := os.Stat(SocketPathFor(kgDir)); !os.IsNotExist(err) {
		t.Errorf("socket file still exists after stop")
	}

	// PID file should be removed.
	if _, err := os.Stat(pidPath(kgDir)); !os.IsNotExist(err) {
		t.Errorf("pid file still exists after stop")
	}
}

func TestFileChangeTriggerSync(t *testing.T) {
	kgDir := setupTestKG(t)

	// Initial sync so manifest exists.
	if err := store.Sync(kgDir); err != nil {
		t.Fatalf("initial Sync: %v", err)
	}

	d, err := Start(kgDir)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer d.Stop()

	// Write an entity file to trigger the watcher.
	e := &store.Entity{
		ID:         "person:test-daemon",
		Type:       "Person",
		Attributes: map[string]any{"name": "Test"},
	}
	if err := store.WriteEntity(kgDir, e); err != nil {
		t.Fatalf("WriteEntity: %v", err)
	}

	// Wait for debounce + sync to complete.
	time.Sleep(2 * time.Second)

	// Verify the entity is in the index.
	idx, err := store.OpenIndex(kgDir)
	if err != nil {
		t.Fatalf("OpenIndex: %v", err)
	}
	defer idx.Close()

	got, err := idx.GetEntity("person:test-daemon")
	if err != nil {
		t.Fatalf("GetEntity: %v", err)
	}
	if got.Type != "Person" {
		t.Errorf("type = %q, want Person", got.Type)
	}
}

func TestPIDFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".cache"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := writePID(dir); err != nil {
		t.Fatalf("writePID: %v", err)
	}

	pid, err := readPID(dir)
	if err != nil {
		t.Fatalf("readPID: %v", err)
	}
	if pid != os.Getpid() {
		t.Errorf("pid = %d, want %d", pid, os.Getpid())
	}

	if !isRunning(pid) {
		t.Error("isRunning(self) = false, want true")
	}

	if err := removePID(dir); err != nil {
		t.Fatalf("removePID: %v", err)
	}

	pid2, err := readPID(dir)
	if err != nil {
		t.Fatalf("readPID after remove: %v", err)
	}
	if pid2 != 0 {
		t.Errorf("pid after remove = %d, want 0", pid2)
	}
}
