package watch

import "time"

// Event types emitted by the watcher.
const (
	EventInit          = "init"
	EventCreated       = "created"
	EventUpdated       = "updated"
	EventDeleted       = "deleted"
	EventLinked        = "linked"
	EventUnlinked      = "unlinked"
	EventSnapshotStart = "snapshot_start"
	EventSnapshotEnd   = "snapshot_end"
)

// Event is the common structure for all watch events.
type Event struct {
	EventType string   `json:"event"`
	Ref       string   `json:"ref,omitempty"`
	Type      string   `json:"type,omitempty"`
	Name      string   `json:"name,omitempty"`
	Changed   []string `json:"changed,omitempty"`
	From      string   `json:"from,omitempty"`
	Rel       string   `json:"rel,omitempty"`
	To        string   `json:"to,omitempty"`
	Snapshot  bool     `json:"snapshot,omitempty"`
	Ts        string   `json:"ts"`

	// Init / snapshot fields
	EntityCount int      `json:"entity_count,omitempty"`
	Types       []string `json:"types,omitempty"`
}

// Now returns the current time formatted as ISO 8601.
func Now() string {
	return time.Now().UTC().Format(time.RFC3339)
}
