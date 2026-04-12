package daemon

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/rpc"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/pbleisch/kg/internal/watch"
	"github.com/pbleisch/kg/pkg/store"
	"gopkg.in/yaml.v3"
)

const debounceDelay = 1 * time.Second

// DefaultIdleTimeout is the duration after which the daemon shuts itself down
// if no file changes or RPC calls have occurred.
const DefaultIdleTimeout = 30 * time.Minute

// Daemon watches .kg/entities/ for file changes and auto-syncs the index.
type Daemon struct {
	kgDir      string
	socketPath string
	rpcPath    string
	listener   net.Listener
	rpcLn      net.Listener
	rpcServer  *rpc.Server
	service    *IndexService
	watcher    *fsnotify.Watcher
	log        *log.Logger
	done         chan struct{}
	wg           sync.WaitGroup
	lastActivity atomic.Int64  // unix timestamp of last activity
	idleTimeout  time.Duration

	// Subscriber management for watch event streaming.
	subMu       sync.Mutex
	subscribers map[net.Conn]struct{}

	cacheMu     sync.RWMutex // guards entityCache
	entityCache map[string]*store.Entity

	pendingMu    sync.Mutex // guards pendingPaths and newDirs
	pendingPaths map[string]fsnotify.Op
	newDirs      map[string]struct{} // dirs added during debounce; re-scanned on sync
}

// Option configures a Daemon.
type Option func(*Daemon)

// WithLogger sets a custom logger. Default is log.Default() (writes to stderr).
func WithLogger(l *log.Logger) Option {
	return func(d *Daemon) { d.log = l }
}

// WithQuiet disables all logging output.
func WithQuiet() Option {
	return func(d *Daemon) { d.log = log.New(io.Discard, "", 0) }
}

// SocketPathFor returns the Unix socket path for the given kgDir.
// Unix domain sockets have a ~104-byte path limit, so we use a short
// deterministic path under the OS temp directory when the default path
// would be too long.
func SocketPathFor(kgDir string) string {
	p := filepath.Join(kgDir, ".cache", "daemon.sock")
	if len(p) < 100 {
		return p
	}
	// Use a hash-based name in the temp dir to stay within the limit.
	h := fmt.Sprintf("%x", sha256Sum(kgDir))[:16]
	return filepath.Join(os.TempDir(), "kg-daemon-"+h+".sock")
}

func sha256Sum(s string) [32]byte {
	return sha256.Sum256([]byte(s))
}

// Start creates and starts a new Daemon for the knowledge graph at kgDir.
func Start(kgDir string, opts ...Option) (*Daemon, error) {
	absDir, err := filepath.Abs(kgDir)
	if err != nil {
		return nil, fmt.Errorf("resolve kg dir: %w", err)
	}

	entitiesDir := filepath.Join(absDir, "entities")
	if _, err := os.Stat(entitiesDir); err != nil {
		return nil, fmt.Errorf("entities dir: %w", err)
	}

	cacheDir := filepath.Join(absDir, ".cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, fmt.Errorf("create cache dir: %w", err)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("create watcher: %w", err)
	}

	// Watch entities dir and all subdirectories.
	if err := watchRecursive(watcher, entitiesDir); err != nil {
		watcher.Close()
		return nil, fmt.Errorf("watch entities: %w", err)
	}

	sockPath := SocketPathFor(absDir)
	// Clean up stale socket file.
	os.Remove(sockPath)

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		watcher.Close()
		return nil, fmt.Errorf("listen socket: %w", err)
	}

	if err := writePID(absDir); err != nil {
		ln.Close()
		watcher.Close()
		return nil, fmt.Errorf("write pid: %w", err)
	}

	// Bypass daemon detection for this call only — we ARE the daemon.
	store.SetOpenIndexDirect(true)
	idx, err := store.OpenIndex(absDir)
	store.SetOpenIndexDirect(false)
	if err != nil {
		ln.Close()
		watcher.Close()
		return nil, fmt.Errorf("open index: %w", err)
	}

	// Set up RPC service.
	svc := NewIndexService(absDir, idx)
	rpcSrv := rpc.NewServer()
	if err := rpcSrv.Register(svc); err != nil {
		idx.Close()
		ln.Close()
		watcher.Close()
		return nil, fmt.Errorf("register rpc: %w", err)
	}

	// Listen on RPC socket.
	rpcSockPath := RPCSocketPath(absDir)
	os.Remove(rpcSockPath)
	rpcLn, err := net.Listen("unix", rpcSockPath)
	if err != nil {
		idx.Close()
		ln.Close()
		watcher.Close()
		return nil, fmt.Errorf("listen rpc socket: %w", err)
	}

	d := &Daemon{
		kgDir:       absDir,
		socketPath:  sockPath,
		rpcPath:     rpcSockPath,
		listener:    ln,
		rpcLn:       rpcLn,
		rpcServer:   rpcSrv,
		service:     svc,
		watcher:     watcher,
		log:         log.New(os.Stdout, "daemon: ", log.Ltime),
		done:        make(chan struct{}),
		idleTimeout: DefaultIdleTimeout,
		subscribers: make(map[net.Conn]struct{}),
		entityCache:  make(map[string]*store.Entity),
		pendingPaths: make(map[string]fsnotify.Op),
		newDirs:      make(map[string]struct{}),
	}

	for _, o := range opts {
		o(d)
	}

	// Share logger with the RPC service.
	svc.log = d.log

	// Pre-populate entity cache for diffing (strip embeddings to save memory).
	if entities, err := store.ListEntities(absDir); err == nil {
		for _, e := range entities {
			stripEmbeddings(e)
			d.entityCache[e.ID] = e
		}
		d.log.Printf("cached %d entities", len(entities))
	}

	d.touch()
	d.wg.Add(4)
	go d.watchLoop()
	go d.acceptLoop()
	go d.rpcAcceptLoop()
	go d.idleLoop()

	d.log.Printf("started (dir=%s, control=%s, rpc=%s)", absDir, sockPath, rpcSockPath)

	return d, nil
}

// Stop shuts down the daemon gracefully.
func (d *Daemon) Stop() error {
	d.log.Printf("stopping")
	close(d.done)
	d.listener.Close()
	d.rpcLn.Close()
	d.watcher.Close()
	d.wg.Wait()
	// Close all subscriber connections.
	d.subMu.Lock()
	nSubs := len(d.subscribers)
	for conn := range d.subscribers {
		conn.Close()
	}
	d.subscribers = nil
	d.subMu.Unlock()
	if nSubs > 0 {
		d.log.Printf("closed %d subscriber(s)", nSubs)
	}
	// Close the held-open index.
	d.service.idx.Close()
	os.Remove(d.socketPath)
	os.Remove(d.rpcPath)
	removePID(d.kgDir)
	d.log.Printf("stopped")
	return nil
}

// Done returns a channel that is closed when the daemon is stopped.
func (d *Daemon) Done() <-chan struct{} {
	return d.done
}

func (d *Daemon) touch() {
	d.lastActivity.Store(time.Now().Unix())
}

func (d *Daemon) idleLoop() {
	defer d.wg.Done()
	if d.idleTimeout <= 0 {
		return // no idle timeout configured
	}
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-d.done:
			return
		case <-ticker.C:
			last := d.lastActivity.Load()
			if last > 0 && time.Since(time.Unix(last, 0)) > d.idleTimeout {
				d.log.Printf("idle for %s, shutting down", d.idleTimeout)
				go d.Stop()
				return
			}
		}
	}
}

func (d *Daemon) watchLoop() {
	defer d.wg.Done()
	var timer *time.Timer
	for {
		select {
		case <-d.done:
			if timer != nil {
				timer.Stop()
			}
			return
		case event, ok := <-d.watcher.Events:
			if !ok {
				return
			}
			// Watch new subdirectories. The actual file scan happens
			// when the debounce fires (syncChangedFiles) to avoid a
			// race where kqueue misses events for files written
			// between watcher.Add and the dir snapshot.
			if event.Has(fsnotify.Create) {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					d.watcher.Add(event.Name)
					d.pendingMu.Lock()
					d.newDirs[event.Name] = struct{}{}
					d.pendingMu.Unlock()
					// Trigger debounce so syncChangedFiles re-scans
					// this directory even if no individual file events
					// arrive (macOS kqueue race).
					if timer != nil {
						timer.Stop()
					}
					timer = time.AfterFunc(debounceDelay, func() {
						d.syncChangedFiles()
					})
				}
			}
			if !isYAML(event.Name) {
				continue
			}
			// Accumulate changed paths during the debounce window.
			d.pendingMu.Lock()
			d.pendingPaths[event.Name] |= event.Op
			d.pendingMu.Unlock()

			if timer != nil {
				timer.Stop()
			}
			timer = time.AfterFunc(debounceDelay, func() {
				d.syncChangedFiles()
			})
		case _, ok := <-d.watcher.Errors:
			if !ok {
				return
			}
			d.log.Printf("watch error")
		}
	}
}

func (d *Daemon) acceptLoop() {
	defer d.wg.Done()
	for {
		conn, err := d.listener.Accept()
		if err != nil {
			select {
			case <-d.done:
				return
			default:
				continue
			}
		}
		go d.handleConn(conn)
	}
}

func (d *Daemon) rpcAcceptLoop() {
	defer d.wg.Done()
	for {
		conn, err := d.rpcLn.Accept()
		if err != nil {
			select {
			case <-d.done:
				return
			default:
				continue
			}
		}
		d.touch()
		go d.rpcServer.ServeConn(conn)
	}
}

func (d *Daemon) handleConn(conn net.Conn) {
	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	if err != nil {
		conn.Close()
		return
	}
	cmd := string(buf[:n])
	switch cmd {
	case "stop":
		d.touch()
		d.log.Printf("stop requested")
		conn.Write([]byte("stopping"))
		conn.Close()
		go d.Stop()
	case "status":
		conn.Write([]byte("running"))
		conn.Close()
	case "subscribe":
		d.touch()
		d.log.Printf("subscriber connected")
		d.addSubscriber(conn)
		// Connection stays open; will be cleaned up on write failure or Stop.
	default:
		conn.Write([]byte("unknown command"))
		conn.Close()
	}
}

func (d *Daemon) addSubscriber(conn net.Conn) {
	d.subMu.Lock()
	d.subscribers[conn] = struct{}{}
	d.subMu.Unlock()
}

func (d *Daemon) removeSubscriber(conn net.Conn) {
	d.subMu.Lock()
	delete(d.subscribers, conn)
	d.subMu.Unlock()
	conn.Close()
}

func (d *Daemon) broadcast(events []watch.Event) {
	d.subMu.Lock()
	subs := make([]net.Conn, 0, len(d.subscribers))
	for conn := range d.subscribers {
		subs = append(subs, conn)
	}
	d.subMu.Unlock()

	for _, conn := range subs {
		for _, ev := range events {
			data, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			data = append(data, '\n')
			if _, err := conn.Write(data); err != nil {
				d.removeSubscriber(conn)
				break
			}
		}
	}
}

// syncChangedFiles processes only the files that changed since the last sync.
// This is dramatically faster than the old syncAndBroadcast which re-read all
// 25K entity files on every change.
func (d *Daemon) syncChangedFiles() {
	d.touch()
	start := time.Now()

	// Drain pending paths and new directories.
	d.pendingMu.Lock()
	paths := d.pendingPaths
	d.pendingPaths = make(map[string]fsnotify.Op, len(paths))
	dirs := d.newDirs
	d.newDirs = make(map[string]struct{}, len(dirs))
	d.pendingMu.Unlock()

	// Re-scan any directories added during this debounce window.
	// On macOS (kqueue), file events can be missed when a file is
	// written between watcher.Add and the directory snapshot.
	for dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			child := filepath.Join(dir, entry.Name())
			if isYAML(child) {
				if _, exists := paths[child]; !exists {
					paths[child] = fsnotify.Create
				}
			}
		}
	}

	if len(paths) == 0 {
		return
	}

	entitiesDir := filepath.Join(d.kgDir, "entities")
	ts := watch.Now()
	var events []watch.Event
	var toUpsert []*store.Entity
	var toRemoveIDs []string

	type manifestEntry struct {
		rel      string
		checksum string
		modTime  time.Time
	}
	var upsertManifest []manifestEntry
	var removeManifestRels []string

	// Process each changed path — all file I/O happens outside the lock.
	for path, op := range paths {
		rel, err := filepath.Rel(d.kgDir, path)
		if err != nil {
			continue
		}

		if op.Has(fsnotify.Remove) || op.Has(fsnotify.Rename) {
			// File deleted.
			d.cacheMu.RLock()
			old, existed := d.entityCache[entityIDFromPath(path, entitiesDir)]
			d.cacheMu.RUnlock()
			if existed {
				events = append(events, watch.Diff(old, nil, ts)...)
				toRemoveIDs = append(toRemoveIDs, old.ID)
			}
			removeManifestRels = append(removeManifestRels, rel)
			continue
		}

		// File created or modified — read it.
		data, err := os.ReadFile(path)
		if err != nil {
			continue // file may have been deleted between event and read
		}
		var e store.Entity
		if err := yaml.Unmarshal(data, &e); err != nil {
			continue
		}

		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		checksum, err := store.FileChecksum(path)
		if err != nil {
			continue
		}

		// Diff against cache for watch events.
		stripped := shallowCopy(&e)
		stripEmbeddings(stripped)

		d.cacheMu.RLock()
		old := d.entityCache[e.ID]
		d.cacheMu.RUnlock()

		events = append(events, watch.Diff(old, stripped, ts)...)

		toUpsert = append(toUpsert, &e)
		upsertManifest = append(upsertManifest, manifestEntry{
			rel:      rel,
			checksum: checksum,
			modTime:  info.ModTime(),
		})
	}

	// Update index — hold write lock only for the actual index operations.
	if len(toUpsert) > 0 || len(toRemoveIDs) > 0 {
		d.service.mu.Lock()
		if len(toUpsert) > 0 {
			if bi, ok := d.service.idx.(store.BatchIndexer); ok {
				bi.UpsertEntities(toUpsert) //nolint:errcheck
			} else {
				for _, e := range toUpsert {
					d.service.idx.UpsertEntity(e) //nolint:errcheck
				}
			}
		}
		if len(toRemoveIDs) > 0 {
			if bi, ok := d.service.idx.(store.BatchIndexer); ok {
				bi.RemoveEntities(toRemoveIDs) //nolint:errcheck
			} else {
				for _, id := range toRemoveIDs {
					d.service.idx.RemoveEntity(id) //nolint:errcheck
				}
			}
		}
		d.service.mu.Unlock()
	}

	// Update entity cache (strip embeddings for memory efficiency).
	d.cacheMu.Lock()
	for _, e := range toUpsert {
		stripped := shallowCopy(e)
		stripEmbeddings(stripped)
		d.entityCache[stripped.ID] = stripped
	}
	for _, id := range toRemoveIDs {
		delete(d.entityCache, id)
	}
	d.cacheMu.Unlock()

	// Update manifest.
	if manifest, err := store.LoadManifest(d.kgDir); err == nil {
		for _, me := range upsertManifest {
			manifest.Files[me.rel] = store.FileEntry{
				Checksum: me.checksum,
				ModTime:  me.modTime,
			}
		}
		for _, rel := range removeManifestRels {
			delete(manifest.Files, rel)
		}
		manifest.SyncedAt = time.Now()
		store.SaveManifest(d.kgDir, manifest) //nolint:errcheck
	}

	d.log.Printf("sync: %d changed, %d upserted, %d removed, %d events (%s)",
		len(paths), len(toUpsert), len(toRemoveIDs), len(events),
		time.Since(start).Round(time.Millisecond))

	if len(events) > 0 {
		d.broadcast(events)
	}
}

// syncAndBroadcast is the full-sync fallback used only during error recovery.
// For normal operation, syncChangedFiles is used instead.
func (d *Daemon) syncAndBroadcast() {
	d.touch()
	start := time.Now()
	// Read current entities.
	newEntities, err := store.ListEntities(d.kgDir)
	if err != nil {
		d.log.Printf("sync: list entities error: %v", err)
		d.service.mu.Lock()
		store.SyncWithIndex(d.kgDir, d.service.idx) //nolint:errcheck
		d.service.mu.Unlock()
		return
	}

	ts := watch.Now()
	newMap := make(map[string]*store.Entity, len(newEntities))
	for _, e := range newEntities {
		stripEmbeddings(e)
		newMap[e.ID] = e
	}

	var events []watch.Event

	d.cacheMu.RLock()
	for id, old := range d.entityCache {
		new, exists := newMap[id]
		if !exists {
			events = append(events, watch.Diff(old, nil, ts)...)
		} else {
			events = append(events, watch.Diff(old, new, ts)...)
		}
	}
	for id, new := range newMap {
		if _, exists := d.entityCache[id]; !exists {
			events = append(events, watch.Diff(nil, new, ts)...)
		}
	}
	d.cacheMu.RUnlock()

	// Update cache.
	d.cacheMu.Lock()
	d.entityCache = newMap
	d.cacheMu.Unlock()

	// Sync index.
	d.service.mu.Lock()
	store.SyncWithIndex(d.kgDir, d.service.idx) //nolint:errcheck
	d.service.mu.Unlock()

	d.log.Printf("sync: %d entities, %d events (%s)", len(newEntities), len(events), time.Since(start).Round(time.Millisecond))

	if len(events) > 0 {
		d.broadcast(events)
	}
}

// entityIDFromPath extracts an entity ID from a filesystem path.
// e.g., "/path/to/.kg/entities/concept/foo.yaml" → "concept:foo"
func entityIDFromPath(path, entitiesDir string) string {
	rel, err := filepath.Rel(entitiesDir, path)
	if err != nil {
		return ""
	}
	parts := strings.SplitN(rel, string(filepath.Separator), 2)
	if len(parts) != 2 {
		return ""
	}
	localID := strings.TrimSuffix(parts[1], ".yaml")
	return parts[0] + ":" + localID
}

// stripEmbeddings removes *_embedding attributes from an entity to reduce
// memory usage in the cache and speed up diff comparisons.
func stripEmbeddings(e *store.Entity) {
	if e.Attributes == nil {
		return
	}
	for k := range e.Attributes {
		if strings.HasSuffix(k, "_embedding") {
			delete(e.Attributes, k)
		}
	}
}

// shallowCopy creates a shallow copy of an entity suitable for caching.
func shallowCopy(e *store.Entity) *store.Entity {
	cp := *e
	if e.Attributes != nil {
		cp.Attributes = make(map[string]any, len(e.Attributes))
		for k, v := range e.Attributes {
			cp.Attributes[k] = v
		}
	}
	if e.Relations != nil {
		cp.Relations = make(map[string][]string, len(e.Relations))
		for k, v := range e.Relations {
			cp.Relations[k] = v
		}
	}
	return &cp
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
