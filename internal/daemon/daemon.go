package daemon

import (
	"crypto/sha256"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
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
		kgDir:      absDir,
		socketPath: sockPath,
		listener:   ln,
		watcher:    watcher,
		done:       make(chan struct{}),
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
			// Watch new subdirectories and trigger sync (files may
			// already exist in the new directory).
			if event.Has(fsnotify.Create) {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					d.watcher.Add(event.Name)
					if timer != nil {
						timer.Stop()
					}
					timer = time.AfterFunc(debounceDelay, func() {
						store.Sync(d.kgDir) //nolint:errcheck
					})
					continue
				}
			}
			if !isYAML(event.Name) {
				continue
			}
			// Debounce: reset timer on each event.
			if timer != nil {
				timer.Stop()
			}
			timer = time.AfterFunc(debounceDelay, func() {
				store.Sync(d.kgDir) //nolint:errcheck
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
	defer conn.Close()
	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	if err != nil {
		return
	}
	cmd := string(buf[:n])
	switch cmd {
	case "stop":
		conn.Write([]byte("stopping"))
		go d.Stop()
	case "status":
		conn.Write([]byte("running"))
	default:
		conn.Write([]byte("unknown command"))
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
