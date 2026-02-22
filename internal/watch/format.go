package watch

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// Encoder writes events to an output stream.
type Encoder interface {
	Encode(Event) error
}

// NDJSONEncoder writes one JSON object per line.
type NDJSONEncoder struct {
	w io.Writer
}

// NewNDJSONEncoder creates an NDJSON encoder writing to w.
func NewNDJSONEncoder(w io.Writer) *NDJSONEncoder {
	return &NDJSONEncoder{w: w}
}

// Encode writes a single event as a JSON line.
func (e *NDJSONEncoder) Encode(ev Event) error {
	data, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = e.w.Write(data)
	return err
}

// HumanEncoder writes human-readable, symbol-prefixed lines.
type HumanEncoder struct {
	w io.Writer
}

// NewHumanEncoder creates a human-readable encoder writing to w.
func NewHumanEncoder(w io.Writer) *HumanEncoder {
	return &HumanEncoder{w: w}
}

// Encode writes a single event as a human-readable line.
func (e *HumanEncoder) Encode(ev Event) error {
	ts := formatTime(ev.Ts)
	var line string
	switch ev.EventType {
	case EventInit:
		line = fmt.Sprintf("[%s] init: %d entities across %d types", ts, ev.EntityCount, len(ev.Types))
	case EventCreated:
		line = fmt.Sprintf("[%s] + %s (%s) %q", ts, ev.Ref, ev.Type, ev.Name)
	case EventUpdated:
		line = fmt.Sprintf("[%s] ~ %s (%s) %q [%s]", ts, ev.Ref, ev.Type, ev.Name, strings.Join(ev.Changed, ", "))
	case EventDeleted:
		line = fmt.Sprintf("[%s] - %s (%s)", ts, ev.Ref, ev.Type)
	case EventLinked:
		line = fmt.Sprintf("[%s] → %s --%s--> %s", ts, ev.From, ev.Rel, ev.To)
	case EventUnlinked:
		line = fmt.Sprintf("[%s] ✕ %s --%s--> %s", ts, ev.From, ev.Rel, ev.To)
	case EventSnapshotStart:
		line = fmt.Sprintf("[%s] snapshot: starting (%d entities)", ts, ev.EntityCount)
	case EventSnapshotEnd:
		line = fmt.Sprintf("[%s] snapshot: complete (%d entities)", ts, ev.EntityCount)
	default:
		line = fmt.Sprintf("[%s] %s", ts, ev.EventType)
	}
	_, err := fmt.Fprintln(e.w, line)
	return err
}

func formatTime(iso string) string {
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return iso
	}
	return t.Local().Format("15:04:05")
}
