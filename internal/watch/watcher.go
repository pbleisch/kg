package watch

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
	"github.com/pbleisch/kg/pkg/store"
)

// Watcher watches .kg/entities/ for changes and emits events.
type Watcher struct {
	kgDir   string
	fw      *fsnotify.Watcher
	events  chan Event
	done    chan struct{}
	wg      sync.WaitGroup
	mu      sync.Mutex
	cache   map[string]*store.Entity // keyed by entity ID
}

// NewWatcher creates a watcher for the given kg directory.
// Call Start() to begin watching and Events() to receive events.
func NewWatcher(kgDir string) (*Watcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	w := &Watcher{
		kgDir:  kgDir,
		fw:     fw,
		events: make(chan Event, 256),
		done:   make(chan struct{}),
		cache:  make(map[string]*store.Entity),
	}

	// Pre-populate cache with current entities.
	entities, _ := store.ListEntities(kgDir)
	for _, e := range entities {
		w.cache[e.ID] = e
	}

	// Watch entities directory recursively.
	entitiesDir := filepath.Join(kgDir, "entities")
	if err := watchRecursive(fw, entitiesDir); err != nil {
		fw.Close()
		return nil, err
	}

	return w, nil
}

// Start begins watching for filesystem events in a goroutine.
func (w *Watcher) Start() {
	w.wg.Add(1)
	go w.loop()
}

// Events returns the channel of watch events.
func (w *Watcher) Events() <-chan Event {
	return w.events
}

// Stop stops the watcher and closes the events channel.
func (w *Watcher) Stop() {
	close(w.done)
	w.fw.Close()
	w.wg.Wait()
	close(w.events)
}

// EntityCache returns a snapshot of the current entity cache.
func (w *Watcher) EntityCache() map[string]*store.Entity {
	w.mu.Lock()
	defer w.mu.Unlock()
	cp := make(map[string]*store.Entity, len(w.cache))
	for k, v := range w.cache {
		cp[k] = v
	}
	return cp
}

func (w *Watcher) loop() {
	defer w.wg.Done()
	for {
		select {
		case <-w.done:
			return
		case ev, ok := <-w.fw.Events:
			if !ok {
				return
			}
			w.handleFSEvent(ev)
		case _, ok := <-w.fw.Errors:
			if !ok {
				return
			}
		}
	}
}

func (w *Watcher) handleFSEvent(ev fsnotify.Event) {
	// Watch new subdirectories.
	if ev.Has(fsnotify.Create) {
		if info, err := os.Stat(ev.Name); err == nil && info.IsDir() {
			w.fw.Add(ev.Name)
			return
		}
	}

	if !isYAML(ev.Name) {
		return
	}

	// Ignore cache directory.
	entitiesDir := filepath.Join(w.kgDir, "entities")
	if !strings.HasPrefix(ev.Name, entitiesDir) {
		return
	}

	id := pathToEntityID(w.kgDir, ev.Name)
	if id == "" {
		return
	}

	ts := Now()
	w.mu.Lock()
	old := w.cache[id]
	w.mu.Unlock()

	if ev.Has(fsnotify.Remove) || ev.Has(fsnotify.Rename) {
		if old != nil {
			for _, e := range Diff(old, nil, ts) {
				w.emit(e)
			}
			w.mu.Lock()
			delete(w.cache, id)
			w.mu.Unlock()
		}
		return
	}

	// Create or Write — read the new state.
	new, err := store.ReadEntity(w.kgDir, id)
	if err != nil {
		return
	}

	events := Diff(old, new, ts)
	for _, e := range events {
		w.emit(e)
	}

	w.mu.Lock()
	w.cache[id] = new
	w.mu.Unlock()
}

func (w *Watcher) emit(ev Event) {
	select {
	case w.events <- ev:
	case <-w.done:
	}
}

// pathToEntityID converts a filesystem path back to a type:local-id.
func pathToEntityID(kgDir, path string) string {
	entitiesDir := filepath.Join(kgDir, "entities") + string(filepath.Separator)
	rel := strings.TrimPrefix(path, entitiesDir)
	if rel == path {
		return ""
	}
	parts := strings.SplitN(rel, string(filepath.Separator), 2)
	if len(parts) != 2 {
		return ""
	}
	typeName := parts[0]
	localID := strings.TrimSuffix(parts[1], ".yaml")
	localID = strings.TrimSuffix(localID, ".yml")
	if typeName == "" || localID == "" {
		return ""
	}
	return typeName + ":" + localID
}

func watchRecursive(w *fsnotify.Watcher, dir string) error {
	return filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return w.Add(path)
		}
		return nil
	})
}

func isYAML(name string) bool {
	ext := filepath.Ext(name)
	return ext == ".yaml" || ext == ".yml"
}
