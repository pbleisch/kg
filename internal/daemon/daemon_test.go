package daemon

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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
	time.Sleep(3 * time.Second)

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

func TestConcurrentStop(t *testing.T) {
	kgDir := setupTestKG(t)

	d, err := Start(kgDir, WithQuiet())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.Stop()
		}()
	}
	wg.Wait()
	// If we reach here without a panic, the sync.Once guard works.
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

func TestSocketBindPreventsSecondDaemon(t *testing.T) {
	kgDir := setupTestKG(t)

	d1, err := Start(kgDir, WithQuiet())
	if err != nil {
		t.Fatalf("Start daemon 1: %v", err)
	}

	// Attempting to start a second daemon on the same kgDir must fail
	// because the first daemon holds the socket lock.
	_, err = Start(kgDir, WithQuiet())
	if err == nil {
		t.Fatal("Start daemon 2: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "already running") && !strings.Contains(err.Error(), "address already in use") {
		t.Fatalf("Start daemon 2: unexpected error: %v", err)
	}

	if err := d1.Stop(); err != nil {
		t.Fatalf("Stop daemon 1: %v", err)
	}

	// Verify daemon 1 cleaned up.
	if _, err := os.Stat(SocketPathFor(kgDir)); !os.IsNotExist(err) {
		t.Error("socket file still exists after stop")
	}
	if _, err := os.Stat(pidPath(kgDir)); !os.IsNotExist(err) {
		t.Error("pid file still exists after stop")
	}
}

func TestCleanStaleRecovery(t *testing.T) {
	kgDir := setupTestKG(t)

	// Start and stop a daemon cleanly so the index exists.
	d1, err := Start(kgDir, WithQuiet())
	if err != nil {
		t.Fatalf("Start daemon 1: %v", err)
	}
	if err := d1.Stop(); err != nil {
		t.Fatalf("Stop daemon 1: %v", err)
	}

	// Simulate a crashed daemon: write a stale PID file pointing to a
	// non-existent process and create stale socket files.
	stalePID := 999999
	if isRunning(stalePID) {
		t.Skip("PID 999999 is actually running; cannot simulate stale state")
	}

	cacheDir := filepath.Join(kgDir, ".cache")
	if err := os.WriteFile(pidPath(kgDir), []byte(strconv.Itoa(stalePID)), 0o644); err != nil {
		t.Fatalf("write stale PID: %v", err)
	}
	if err := os.WriteFile(SocketPathFor(kgDir), []byte("stale"), 0o644); err != nil {
		t.Fatalf("create stale socket: %v", err)
	}
	if err := os.WriteFile(RPCSocketPath(kgDir), []byte("stale"), 0o644); err != nil {
		t.Fatalf("create stale rpc socket: %v", err)
	}
	_ = cacheDir // used implicitly via pidPath/SocketPathFor

	// Start a new daemon — cleanStale should remove the stale files and
	// allow the new daemon to bind the socket.
	d2, err := Start(kgDir, WithQuiet())
	if err != nil {
		t.Fatalf("Start daemon 2 after stale cleanup: %v", err)
	}

	// Verify the new daemon is functional: PID file should reflect current process.
	pid, err := readPID(kgDir)
	if err != nil {
		t.Fatalf("readPID: %v", err)
	}
	if pid != os.Getpid() {
		t.Errorf("pid = %d, want %d", pid, os.Getpid())
	}

	if err := d2.Stop(); err != nil {
		t.Fatalf("Stop daemon 2: %v", err)
	}
}
