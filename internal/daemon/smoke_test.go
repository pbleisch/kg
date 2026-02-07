//go:build integration

package daemon

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/pbleisch/kg/pkg/store"
)

func TestSmoke_DaemonSyncsNewEntity(t *testing.T) {
	kgDir := setupTestKG(t)

	// Initial sync so manifest exists.
	if err := store.Sync(kgDir); err != nil {
		t.Fatalf("initial Sync: %v", err)
	}

	goroutinesBefore := runtime.NumGoroutine()

	d, err := Start(kgDir)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Write a new entity file into a new type subdirectory.
	// WriteEntity creates entities/<type>/<local>.yaml and the type subdir,
	// which triggers the watcher (Create dir + Create file).
	e := &store.Entity{
		ID:   "tool:smoke-test",
		Type: "Tool",
		Attributes: map[string]any{
			"name":    "SmokeTest",
			"version": "1.0",
		},
	}
	if err := store.WriteEntity(kgDir, e); err != nil {
		t.Fatalf("WriteEntity: %v", err)
	}

	// Poll until the entity appears in the index (debounce 100ms + sync time).
	const (
		timeout  = 5 * time.Second
		interval = 50 * time.Millisecond
	)
	deadline := time.Now().Add(timeout)
	var synced bool
	for time.Now().Before(deadline) {
		idx, err := store.OpenIndex(kgDir)
		if err != nil {
			time.Sleep(interval)
			continue
		}
		got, err := idx.GetEntity("tool:smoke-test")
		idx.Close()
		if err == nil && got.Type == "Tool" {
			synced = true
			break
		}
		time.Sleep(interval)
	}
	if !synced {
		t.Fatal("entity tool:smoke-test was not synced to index within timeout")
	}

	// Verify attributes were synced correctly.
	idx, err := store.OpenIndex(kgDir)
	if err != nil {
		t.Fatalf("OpenIndex: %v", err)
	}
	got, err := idx.GetEntity("tool:smoke-test")
	idx.Close()
	if err != nil {
		t.Fatalf("GetEntity: %v", err)
	}
	if got.Attributes["name"] != "SmokeTest" {
		t.Errorf("name = %v, want SmokeTest", got.Attributes["name"])
	}

	// Stop daemon and verify clean shutdown.
	if err := d.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Verify socket and PID file are cleaned up.
	if _, err := os.Stat(SocketPathFor(kgDir)); !os.IsNotExist(err) {
		t.Error("socket file still exists after stop")
	}
	if _, err := os.Stat(filepath.Join(kgDir, ".cache", "daemon.pid")); !os.IsNotExist(err) {
		t.Error("pid file still exists after stop")
	}

	// Verify no goroutine leaks: give goroutines a moment to exit.
	time.Sleep(100 * time.Millisecond)
	goroutinesAfter := runtime.NumGoroutine()
	// Allow a small margin for runtime goroutines.
	if goroutinesAfter > goroutinesBefore+2 {
		t.Errorf("goroutine leak: before=%d after=%d", goroutinesBefore, goroutinesAfter)
	}
}
