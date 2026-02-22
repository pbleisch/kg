package watch

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pbleisch/kg/pkg/store"
)

// --- Event serialization tests ---

func TestEventJSONRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		ev   Event
	}{
		{
			name: "init",
			ev:   Event{EventType: EventInit, EntityCount: 42, Types: []string{"Game", "Mechanic"}, Ts: "2026-01-01T00:00:00Z"},
		},
		{
			name: "created",
			ev:   Event{EventType: EventCreated, Ref: "game:hades", Type: "Game", Name: "Hades", Ts: "2026-01-01T00:00:00Z"},
		},
		{
			name: "updated",
			ev:   Event{EventType: EventUpdated, Ref: "game:hades", Type: "Game", Name: "Hades", Changed: []string{"description"}, Ts: "2026-01-01T00:00:00Z"},
		},
		{
			name: "deleted",
			ev:   Event{EventType: EventDeleted, Ref: "game:hades", Type: "Game", Ts: "2026-01-01T00:00:00Z"},
		},
		{
			name: "linked",
			ev:   Event{EventType: EventLinked, From: "game:hades", Rel: "uses", To: "mechanic:dash", Ts: "2026-01-01T00:00:00Z"},
		},
		{
			name: "unlinked",
			ev:   Event{EventType: EventUnlinked, From: "game:hades", Rel: "uses", To: "mechanic:dash", Ts: "2026-01-01T00:00:00Z"},
		},
		{
			name: "snapshot_created",
			ev:   Event{EventType: EventCreated, Ref: "game:hades", Type: "Game", Name: "Hades", Snapshot: true, Ts: "2026-01-01T00:00:00Z"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.ev)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var got Event
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got.EventType != tt.ev.EventType {
				t.Errorf("event type: got %q, want %q", got.EventType, tt.ev.EventType)
			}
			if got.Ref != tt.ev.Ref {
				t.Errorf("ref: got %q, want %q", got.Ref, tt.ev.Ref)
			}
			if got.Snapshot != tt.ev.Snapshot {
				t.Errorf("snapshot: got %v, want %v", got.Snapshot, tt.ev.Snapshot)
			}
		})
	}
}

func TestEventOmitsEmptyFields(t *testing.T) {
	ev := Event{EventType: EventInit, EntityCount: 5, Ts: "2026-01-01T00:00:00Z"}
	data, _ := json.Marshal(ev)
	s := string(data)
	if strings.Contains(s, `"ref"`) {
		t.Error("init event should not contain ref field")
	}
	if strings.Contains(s, `"snapshot"`) {
		t.Error("non-snapshot event should not contain snapshot field")
	}
}

// --- Formatter tests ---

func TestNDJSONEncoder(t *testing.T) {
	var buf bytes.Buffer
	enc := NewNDJSONEncoder(&buf)
	ev := Event{EventType: EventCreated, Ref: "game:hades", Type: "Game", Name: "Hades", Ts: "2026-01-01T00:00:00Z"}
	if err := enc.Encode(ev); err != nil {
		t.Fatal(err)
	}
	line := buf.String()
	if !strings.HasSuffix(line, "\n") {
		t.Error("NDJSON line should end with newline")
	}
	var got Event
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if got.EventType != EventCreated {
		t.Errorf("got event %q, want %q", got.EventType, EventCreated)
	}
}

func TestHumanEncoder(t *testing.T) {
	tests := []struct {
		ev       Event
		contains string
	}{
		{Event{EventType: EventInit, EntityCount: 10, Types: []string{"A", "B"}, Ts: "2026-01-01T00:00:00Z"}, "init: 10 entities across 2 types"},
		{Event{EventType: EventCreated, Ref: "game:hades", Type: "Game", Name: "Hades", Ts: "2026-01-01T00:00:00Z"}, "+ game:hades (Game)"},
		{Event{EventType: EventUpdated, Ref: "game:hades", Type: "Game", Name: "Hades", Changed: []string{"desc"}, Ts: "2026-01-01T00:00:00Z"}, "~ game:hades"},
		{Event{EventType: EventDeleted, Ref: "game:hades", Type: "Game", Ts: "2026-01-01T00:00:00Z"}, "- game:hades"},
		{Event{EventType: EventLinked, From: "a:b", Rel: "uses", To: "c:d", Ts: "2026-01-01T00:00:00Z"}, "→ a:b --uses--> c:d"},
		{Event{EventType: EventUnlinked, From: "a:b", Rel: "uses", To: "c:d", Ts: "2026-01-01T00:00:00Z"}, "✕ a:b --uses--> c:d"},
	}
	for _, tt := range tests {
		var buf bytes.Buffer
		enc := NewHumanEncoder(&buf)
		enc.Encode(tt.ev)
		if !strings.Contains(buf.String(), tt.contains) {
			t.Errorf("output %q does not contain %q", buf.String(), tt.contains)
		}
	}
}

// --- Differ tests ---

func TestDiffCreated(t *testing.T) {
	e := &store.Entity{
		ID:         "game:hades",
		Type:       "Game",
		Attributes: map[string]any{"name": "Hades"},
		Relations:  map[string][]string{"uses": {"mechanic:dash"}},
	}
	events := Diff(nil, e, "2026-01-01T00:00:00Z")
	if len(events) != 2 {
		t.Fatalf("expected 2 events (created + linked), got %d", len(events))
	}
	if events[0].EventType != EventCreated {
		t.Errorf("first event: got %q, want created", events[0].EventType)
	}
	if events[0].Name != "Hades" {
		t.Errorf("name: got %q, want Hades", events[0].Name)
	}
	if events[1].EventType != EventLinked {
		t.Errorf("second event: got %q, want linked", events[1].EventType)
	}
}

func TestDiffDeleted(t *testing.T) {
	e := &store.Entity{
		ID:        "game:hades",
		Type:      "Game",
		Relations: map[string][]string{"uses": {"mechanic:dash"}},
	}
	events := Diff(e, nil, "2026-01-01T00:00:00Z")
	if len(events) != 2 {
		t.Fatalf("expected 2 events (deleted + unlinked), got %d", len(events))
	}
	if events[0].EventType != EventDeleted {
		t.Errorf("first event: got %q, want deleted", events[0].EventType)
	}
	if events[1].EventType != EventUnlinked {
		t.Errorf("second event: got %q, want unlinked", events[1].EventType)
	}
}

func TestDiffUpdatedAttributes(t *testing.T) {
	old := &store.Entity{ID: "game:hades", Type: "Game", Attributes: map[string]any{"name": "Hades", "year": 2020}}
	new := &store.Entity{ID: "game:hades", Type: "Game", Attributes: map[string]any{"name": "Hades", "year": 2021}}
	events := Diff(old, new, "2026-01-01T00:00:00Z")
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].EventType != EventUpdated {
		t.Errorf("got %q, want updated", events[0].EventType)
	}
	if len(events[0].Changed) != 1 || events[0].Changed[0] != "year" {
		t.Errorf("changed: got %v, want [year]", events[0].Changed)
	}
}

func TestDiffUpdatedRelations(t *testing.T) {
	old := &store.Entity{ID: "game:hades", Type: "Game", Relations: map[string][]string{"uses": {"mechanic:dash"}}}
	new := &store.Entity{ID: "game:hades", Type: "Game", Relations: map[string][]string{"uses": {"mechanic:dash", "mechanic:cast"}}}
	events := Diff(old, new, "2026-01-01T00:00:00Z")
	// Should get: updated (with relations in changed) + linked
	hasUpdated := false
	hasLinked := false
	for _, ev := range events {
		if ev.EventType == EventUpdated {
			hasUpdated = true
		}
		if ev.EventType == EventLinked && ev.To == "mechanic:cast" {
			hasLinked = true
		}
	}
	if !hasUpdated {
		t.Error("expected an updated event")
	}
	if !hasLinked {
		t.Error("expected a linked event for mechanic:cast")
	}
}

func TestDiffNoChange(t *testing.T) {
	e := &store.Entity{ID: "game:hades", Type: "Game", Attributes: map[string]any{"name": "Hades"}}
	events := Diff(e, e, "2026-01-01T00:00:00Z")
	if len(events) != 0 {
		t.Errorf("expected 0 events for identical entities, got %d", len(events))
	}
}

func TestDiffNilNil(t *testing.T) {
	events := Diff(nil, nil, "2026-01-01T00:00:00Z")
	if events != nil {
		t.Error("expected nil for nil/nil diff")
	}
}

// --- Debouncer tests ---

func TestDebouncerCoalesceCreatedUpdated(t *testing.T) {
	in := make(chan Event, 10)
	d := NewDebouncer(in, 50*time.Millisecond)
	d.Start()

	in <- Event{EventType: EventCreated, Ref: "game:hades", Type: "Game", Ts: "t1"}
	in <- Event{EventType: EventUpdated, Ref: "game:hades", Type: "Game", Changed: []string{"name"}, Ts: "t2"}
	close(in)

	var events []Event
	for ev := range d.Events() {
		events = append(events, ev)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 coalesced event, got %d", len(events))
	}
	if events[0].EventType != EventCreated {
		t.Errorf("expected created, got %q", events[0].EventType)
	}
}

func TestDebouncerCoalesceCreatedDeleted(t *testing.T) {
	in := make(chan Event, 10)
	d := NewDebouncer(in, 50*time.Millisecond)
	d.Start()

	in <- Event{EventType: EventCreated, Ref: "game:hades", Ts: "t1"}
	in <- Event{EventType: EventDeleted, Ref: "game:hades", Ts: "t2"}
	close(in)

	var events []Event
	for ev := range d.Events() {
		events = append(events, ev)
	}
	if len(events) != 0 {
		t.Fatalf("expected 0 events (created+deleted suppressed), got %d", len(events))
	}
}

func TestDebouncerMergesUpdated(t *testing.T) {
	in := make(chan Event, 10)
	d := NewDebouncer(in, 50*time.Millisecond)
	d.Start()

	in <- Event{EventType: EventUpdated, Ref: "game:hades", Changed: []string{"name"}, Ts: "t1"}
	in <- Event{EventType: EventUpdated, Ref: "game:hades", Changed: []string{"year"}, Ts: "t2"}
	close(in)

	var events []Event
	for ev := range d.Events() {
		events = append(events, ev)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 merged event, got %d", len(events))
	}
	if len(events[0].Changed) != 2 {
		t.Errorf("expected 2 changed fields, got %v", events[0].Changed)
	}
}

func TestDebouncerPassthroughInit(t *testing.T) {
	in := make(chan Event, 10)
	d := NewDebouncer(in, 50*time.Millisecond)
	d.Start()

	in <- Event{EventType: EventInit, EntityCount: 5, Ts: "t1"}
	close(in)

	var events []Event
	for ev := range d.Events() {
		events = append(events, ev)
	}
	if len(events) != 1 || events[0].EventType != EventInit {
		t.Errorf("init event should pass through immediately")
	}
}

// --- Integration test: watcher ---

func TestWatcherIntegration(t *testing.T) {
	// Create temp kg directory.
	dir := t.TempDir()
	kgDir := filepath.Join(dir, ".kg")
	entitiesDir := filepath.Join(kgDir, "entities", "Game")
	os.MkdirAll(entitiesDir, 0o755)

	// Write an initial entity.
	writeTestEntity(t, kgDir, &store.Entity{
		ID:         "Game:hades",
		Type:       "Game",
		Attributes: map[string]any{"name": "Hades"},
	})

	// Create and start watcher.
	w, err := NewWatcher(kgDir)
	if err != nil {
		t.Fatalf("new watcher: %v", err)
	}
	w.Start()
	defer w.Stop()

	// Allow watcher to settle.
	time.Sleep(100 * time.Millisecond)

	// Create a new entity.
	writeTestEntity(t, kgDir, &store.Entity{
		ID:         "Game:celeste",
		Type:       "Game",
		Attributes: map[string]any{"name": "Celeste"},
	})

	ev := waitForEvent(t, w.Events(), 2*time.Second)
	if ev.EventType != EventCreated {
		t.Errorf("expected created event, got %q", ev.EventType)
	}
	if ev.Ref != "Game:celeste" {
		t.Errorf("expected ref Game:celeste, got %q", ev.Ref)
	}

	// Update the entity.
	writeTestEntity(t, kgDir, &store.Entity{
		ID:         "Game:celeste",
		Type:       "Game",
		Attributes: map[string]any{"name": "Celeste", "year": 2018},
	})

	ev = waitForEvent(t, w.Events(), 2*time.Second)
	if ev.EventType != EventUpdated {
		t.Errorf("expected updated event, got %q", ev.EventType)
	}

	// Delete the entity.
	path := filepath.Join(entitiesDir, "celeste.yaml")
	os.Remove(path)

	ev = waitForEvent(t, w.Events(), 2*time.Second)
	if ev.EventType != EventDeleted {
		t.Errorf("expected deleted event, got %q", ev.EventType)
	}
}

func writeTestEntity(t *testing.T, kgDir string, e *store.Entity) {
	t.Helper()
	if err := store.WriteEntity(kgDir, e); err != nil {
		t.Fatalf("write entity: %v", err)
	}
}

func waitForEvent(t *testing.T, ch <-chan Event, timeout time.Duration) Event {
	t.Helper()
	select {
	case ev := <-ch:
		return ev
	case <-time.After(timeout):
		t.Fatal("timed out waiting for event")
		return Event{}
	}
}
