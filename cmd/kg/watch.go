package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/pbleisch/kg/internal/daemon"
	"github.com/pbleisch/kg/internal/watch"
	"github.com/pbleisch/kg/pkg/store"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func init() {
	watchCmd := &cobra.Command{
		Use:   "watch",
		Short: "Stream entity and relation change events",
		Long:  "Watch the knowledge graph for changes and emit structured events to stdout.",
		RunE:  runWatch,
	}

	watchCmd.Flags().Bool("snapshot", false, "emit all current entities as created events on startup")
	watchCmd.Flags().String("type", "", "comma-separated entity type filter")
	watchCmd.Flags().Duration("debounce", 0, "coalesce rapid changes (e.g., 100ms)")
	watchCmd.Flags().Bool("daemon", false, "subscribe to running kg daemon instead of watching directly")

	rootCmd.AddCommand(watchCmd)
}

func runWatch(cmd *cobra.Command, args []string) error {
	kgDir := cfg.kgDir

	// Verify entities directory exists.
	if _, err := os.Stat(kgDir); err != nil {
		return fmt.Errorf("knowledge graph directory not found: %s", kgDir)
	}

	// Determine output format.
	jsonFlag, _ := cmd.Flags().GetBool("json")
	humanFlag, _ := cmd.Flags().GetBool("human")
	var enc watch.Encoder
	switch {
	case jsonFlag:
		enc = watch.NewNDJSONEncoder(os.Stdout)
	case humanFlag:
		enc = watch.NewHumanEncoder(os.Stdout)
	default:
		// Auto-detect: JSON when piped, human when TTY.
		if term.IsTerminal(int(os.Stdout.Fd())) {
			enc = watch.NewHumanEncoder(os.Stdout)
		} else {
			enc = watch.NewNDJSONEncoder(os.Stdout)
		}
	}

	// Parse type filter.
	typeFilterStr, _ := cmd.Flags().GetString("type")
	var typeFilter map[string]bool
	if typeFilterStr != "" {
		typeFilter = make(map[string]bool)
		for _, t := range strings.Split(typeFilterStr, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				typeFilter[t] = true
			}
		}
	}

	// Check for daemon mode.
	daemonFlag, _ := cmd.Flags().GetBool("daemon")
	if daemonFlag {
		return runWatchDaemon(enc, typeFilter)
	}

	// Create watcher.
	w, err := watch.NewWatcher(kgDir)
	if err != nil {
		return fmt.Errorf("creating watcher: %w", err)
	}

	snapshotFlag, _ := cmd.Flags().GetBool("snapshot")
	debounce, _ := cmd.Flags().GetDuration("debounce")

	// Emit snapshot before starting live events.
	if snapshotFlag {
		if err := emitSnapshot(w, enc, typeFilter); err != nil {
			w.Stop()
			return err
		}
	}

	// Emit init event.
	entities, _ := store.ListEntities(kgDir)
	typeSet := make(map[string]bool)
	for _, e := range entities {
		typeSet[e.Type] = true
	}
	var types []string
	for t := range typeSet {
		types = append(types, t)
	}
	sort.Strings(types)

	enc.Encode(watch.Event{
		EventType:   watch.EventInit,
		EntityCount: len(entities),
		Types:       types,
		Ts:          watch.Now(),
	})

	// Start the watcher.
	w.Start()

	// Set up event channel (optionally with debouncing).
	var eventCh <-chan watch.Event
	var debouncer *watch.Debouncer
	if debounce > 0 {
		debouncer = watch.NewDebouncer(w.Events(), debounce)
		debouncer.Start()
		eventCh = debouncer.Events()
	} else {
		eventCh = w.Events()
	}

	// Signal handling.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Main event loop.
	for {
		select {
		case ev, ok := <-eventCh:
			if !ok {
				return nil
			}
			if matchesTypeFilter(ev, typeFilter) {
				enc.Encode(ev)
			}
		case <-sigCh:
			if debouncer != nil {
				debouncer.Stop()
			}
			w.Stop()
			return nil
		}
	}
}

func emitSnapshot(w *watch.Watcher, enc watch.Encoder, typeFilter map[string]bool) error {
	cache := w.EntityCache()

	// Count entities that pass the filter.
	var filtered []*store.Entity
	for _, e := range cache {
		if typeFilter == nil || typeFilter[e.Type] {
			filtered = append(filtered, e)
		}
	}

	ts := watch.Now()
	enc.Encode(watch.Event{
		EventType:   watch.EventSnapshotStart,
		EntityCount: len(filtered),
		Ts:          ts,
	})

	// Sort for deterministic output.
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].ID < filtered[j].ID
	})

	for _, e := range filtered {
		enc.Encode(watch.Event{
			EventType: watch.EventCreated,
			Ref:       e.ID,
			Type:      e.Type,
			Name:      entityName(e),
			Snapshot:  true,
			Ts:        ts,
		})
	}

	enc.Encode(watch.Event{
		EventType:   watch.EventSnapshotEnd,
		EntityCount: len(filtered),
		Ts:          ts,
	})

	return nil
}

func matchesTypeFilter(ev watch.Event, typeFilter map[string]bool) bool {
	if typeFilter == nil {
		return true
	}
	switch ev.EventType {
	case watch.EventInit, watch.EventSnapshotStart, watch.EventSnapshotEnd:
		return true
	case watch.EventLinked, watch.EventUnlinked:
		fromType := entityTypeFromRef(ev.From)
		toType := entityTypeFromRef(ev.To)
		return typeFilter[fromType] || typeFilter[toType]
	default:
		return typeFilter[ev.Type]
	}
}

func entityTypeFromRef(ref string) string {
	parts := strings.SplitN(ref, ":", 2)
	if len(parts) != 2 {
		return ""
	}
	return parts[0]
}

func entityName(e *store.Entity) string {
	if e.Attributes == nil {
		return ""
	}
	if name, ok := e.Attributes["name"]; ok {
		return fmt.Sprintf("%v", name)
	}
	return ""
}

func runWatchDaemon(enc watch.Encoder, typeFilter map[string]bool) error {
	absDir, err := filepath.Abs(cfg.kgDir)
	if err != nil {
		return err
	}
	sockPath := daemon.SocketPathFor(absDir)
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		return fmt.Errorf("daemon not running (could not connect to %s): %w\nStart it with: kg daemon start", sockPath, err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte("subscribe")); err != nil {
		return fmt.Errorf("subscribing to daemon: %w", err)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Read NDJSON lines from daemon.
	done := make(chan struct{})
	go func() {
		defer close(done)
		scanner := bufio.NewScanner(conn)
		for scanner.Scan() {
			var ev watch.Event
			if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
				continue
			}
			if matchesTypeFilter(ev, typeFilter) {
				enc.Encode(ev)
			}
		}
	}()

	select {
	case <-sigCh:
		return nil
	case <-done:
		return nil
	}
}
