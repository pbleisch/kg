package watch

import (
	"sort"
	"sync"
	"time"
)

// Debouncer buffers events and coalesces them per entity.
type Debouncer struct {
	delay  time.Duration
	in     <-chan Event
	out    chan Event
	done   chan struct{}
	wg     sync.WaitGroup
}

// NewDebouncer creates a debouncer that reads from in and writes
// coalesced events to its output channel after delay.
func NewDebouncer(in <-chan Event, delay time.Duration) *Debouncer {
	return &Debouncer{
		delay: delay,
		in:    in,
		out:   make(chan Event, 256),
		done:  make(chan struct{}),
	}
}

// Start begins the debounce loop.
func (d *Debouncer) Start() {
	d.wg.Add(1)
	go d.loop()
}

// Events returns the debounced event channel.
func (d *Debouncer) Events() <-chan Event {
	return d.out
}

// Stop stops the debouncer and waits for it to finish.
func (d *Debouncer) Stop() {
	close(d.done)
	d.wg.Wait()
}

func (d *Debouncer) loop() {
	defer d.wg.Done()
	defer close(d.out)

	type entityState struct {
		events []Event
	}

	buf := make(map[string]*entityState) // keyed by entity ref or relation key
	var timer *time.Timer
	var timerC <-chan time.Time

	flush := func() {
		for _, st := range buf {
			coalesced := coalesce(st.events)
			for _, ev := range coalesced {
				select {
				case d.out <- ev:
				case <-d.done:
					return
				}
			}
		}
		buf = make(map[string]*entityState)
	}

	for {
		select {
		case <-d.done:
			flush()
			return
		case ev, ok := <-d.in:
			if !ok {
				flush()
				return
			}
			// Pass through non-entity events immediately.
			if ev.EventType == EventInit || ev.EventType == EventSnapshotStart || ev.EventType == EventSnapshotEnd {
				select {
				case d.out <- ev:
				case <-d.done:
					return
				}
				continue
			}

			key := eventKey(ev)
			st, ok := buf[key]
			if !ok {
				st = &entityState{}
				buf[key] = st
			}
			st.events = append(st.events, ev)

			// Reset the flush timer.
			if timer != nil {
				timer.Stop()
			}
			timer = time.NewTimer(d.delay)
			timerC = timer.C
		case <-timerC:
			flush()
			timerC = nil
		}
	}
}

func eventKey(ev Event) string {
	switch ev.EventType {
	case EventLinked, EventUnlinked:
		return ev.From + "|" + ev.Rel + "|" + ev.To
	default:
		return ev.Ref
	}
}

// coalesce reduces a sequence of events for the same entity.
func coalesce(events []Event) []Event {
	if len(events) == 0 {
		return nil
	}

	first := events[0]
	last := events[len(events)-1]

	// Created then Deleted → suppress.
	if first.EventType == EventCreated && last.EventType == EventDeleted {
		return nil
	}

	// Linked then Unlinked → suppress (and vice versa).
	if first.EventType == EventLinked && last.EventType == EventUnlinked {
		return nil
	}
	if first.EventType == EventUnlinked && last.EventType == EventLinked {
		return nil
	}

	// Created then Updated(s) → single Created.
	if first.EventType == EventCreated {
		return []Event{first}
	}

	// Multiple Updated → merge changed fields.
	if first.EventType == EventUpdated && last.EventType == EventUpdated {
		merged := mergeChanged(events)
		result := last
		result.Changed = merged
		return []Event{result}
	}

	// Default: return last event.
	return []Event{last}
}

func mergeChanged(events []Event) []string {
	seen := make(map[string]bool)
	for _, ev := range events {
		for _, c := range ev.Changed {
			seen[c] = true
		}
	}
	var result []string
	for c := range seen {
		result = append(result, c)
	}
	sort.Strings(result)
	return result
}
