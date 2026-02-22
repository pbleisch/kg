package daemon

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/pbleisch/kg/internal/watch"
	"github.com/pbleisch/kg/pkg/store"
)

const debounceDelay = 100 * time.Millisecond

// Daemon watches .kg/entities/ for file changes and auto-syncs the index.
type Daemon struct {
	kgDir      string
	socketPath string
	listener   net.Listener
	watcher    *fsnotify.Watcher
	done       chan struct{}
	wg         sync.WaitGroup

	// Subscriber management for watch event streaming.
	subMu       sync.Mutex
	subscribers map[net.Conn]struct{}
	entityCache map[string]*store.Entity
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
func Start(kgDir string) (*Daemon, error) {
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

	d := &Daemon{
		kgDir:       absDir,
		socketPath:  sockPath,
		listener:    ln,
		watcher:     watcher,
		done:        make(chan struct{}),
		subscribers: make(map[net.Conn]struct{}),
		entityCache: make(map[string]*store.Entity),
	}

	// Pre-populate entity cache for diffing.
	if entities, err := store.ListEntities(absDir); err == nil {
		for _, e := range entities {
			d.entityCache[e.ID] = e
		}
	}

	d.wg.Add(2)
	go d.watchLoop()
	go d.acceptLoop()

	return d, nil
}

// Stop shuts down the daemon gracefully.
func (d *Daemon) Stop() error {
	close(d.done)
	d.listener.Close()
	d.watcher.Close()
	d.wg.Wait()
	// Close all subscriber connections.
	d.subMu.Lock()
	for conn := range d.subscribers {
		conn.Close()
	}
	d.subscribers = nil
	d.subMu.Unlock()
	os.Remove(d.socketPath)
	removePID(d.kgDir)
	return nil
}

// Done returns a channel that is closed when the daemon is stopped.
func (d *Daemon) Done() <-chan struct{} {
	return d.done
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
			// Watch new subdirectories.
			if event.Has(fsnotify.Create) {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					d.watcher.Add(event.Name)
					if timer != nil {
						timer.Stop()
					}
					timer = time.AfterFunc(debounceDelay, func() {
						d.syncAndBroadcast()
					})
					continue
				}
			}
			if !isYAML(event.Name) {
				continue
			}
			if timer != nil {
				timer.Stop()
			}
			timer = time.AfterFunc(debounceDelay, func() {
				d.syncAndBroadcast()
			})
		case _, ok := <-d.watcher.Errors:
			if !ok {
				return
			}
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
		conn.Write([]byte("stopping"))
		conn.Close()
		go d.Stop()
	case "status":
		conn.Write([]byte("running"))
		conn.Close()
	case "subscribe":
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

func (d *Daemon) syncAndBroadcast() {
	// Read current entities.
	newEntities, err := store.ListEntities(d.kgDir)
	if err != nil {
		store.Sync(d.kgDir) //nolint:errcheck
		return
	}

	ts := watch.Now()
	newMap := make(map[string]*store.Entity, len(newEntities))
	for _, e := range newEntities {
		newMap[e.ID] = e
	}

	var events []watch.Event

	// Detect updates and deletions.
	for id, old := range d.entityCache {
		new, exists := newMap[id]
		if !exists {
			events = append(events, watch.Diff(old, nil, ts)...)
		} else {
			events = append(events, watch.Diff(old, new, ts)...)
		}
	}

	// Detect creations.
	for id, new := range newMap {
		if _, exists := d.entityCache[id]; !exists {
			events = append(events, watch.Diff(nil, new, ts)...)
		}
	}

	// Update cache.
	d.entityCache = newMap

	// Sync index.
	store.Sync(d.kgDir) //nolint:errcheck

	// Broadcast to subscribers.
	if len(events) > 0 {
		d.broadcast(events)
	}
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
