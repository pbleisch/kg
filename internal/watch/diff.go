package watch

import (
	"fmt"
	"sort"

	"github.com/pbleisch/kg/pkg/store"
)

// Diff compares two entity states and returns the resulting events.
// If old is nil, a Created event is returned.
// If new is nil, a Deleted event is returned.
// If both are non-nil, an Updated event (if attributes changed) plus
// Linked/Unlinked events (if relations changed) are returned.
func Diff(old, new *store.Entity, ts string) []Event {
	if old == nil && new == nil {
		return nil
	}
	if old == nil {
		return diffCreated(new, ts)
	}
	if new == nil {
		return diffDeleted(old, ts)
	}
	return diffUpdated(old, new, ts)
}

func diffCreated(e *store.Entity, ts string) []Event {
	events := []Event{{
		EventType: EventCreated,
		Ref:       e.ID,
		Type:      e.Type,
		Name:      entityName(e),
		Ts:        ts,
	}}
	// Emit linked events for all initial relations.
	for rel, targets := range e.Relations {
		for _, target := range targets {
			events = append(events, Event{
				EventType: EventLinked,
				From:      e.ID,
				Rel:       rel,
				To:        target,
				Ts:        ts,
			})
		}
	}
	return events
}

func diffDeleted(e *store.Entity, ts string) []Event {
	events := []Event{{
		EventType: EventDeleted,
		Ref:       e.ID,
		Type:      e.Type,
		Ts:        ts,
	}}
	// Emit unlinked events for all removed relations.
	for rel, targets := range e.Relations {
		for _, target := range targets {
			events = append(events, Event{
				EventType: EventUnlinked,
				From:      e.ID,
				Rel:       rel,
				To:        target,
				Ts:        ts,
			})
		}
	}
	return events
}

func diffUpdated(old, new *store.Entity, ts string) []Event {
	var events []Event
	var changed []string

	// Compare attributes.
	if !mapsEqual(old.Attributes, new.Attributes) {
		changed = append(changed, changedAttrs(old.Attributes, new.Attributes)...)
	}

	// Compare relations and emit link/unlink events.
	added, removed := diffRelations(old.Relations, new.Relations)
	if len(added) > 0 || len(removed) > 0 {
		changed = append(changed, "relations")
	}

	for _, r := range removed {
		events = append(events, Event{
			EventType: EventUnlinked,
			From:      old.ID,
			Rel:       r.rel,
			To:        r.target,
			Ts:        ts,
		})
	}
	for _, r := range added {
		events = append(events, Event{
			EventType: EventLinked,
			From:      new.ID,
			Rel:       r.rel,
			To:        r.target,
			Ts:        ts,
		})
	}

	if len(changed) > 0 {
		sort.Strings(changed)
		events = append([]Event{{
			EventType: EventUpdated,
			Ref:       new.ID,
			Type:      new.Type,
			Name:      entityName(new),
			Changed:   changed,
			Ts:        ts,
		}}, events...)
	}

	return events
}

type relEdge struct {
	rel    string
	target string
}

func diffRelations(old, new map[string][]string) (added, removed []relEdge) {
	oldSet := relSet(old)
	newSet := relSet(new)

	for edge := range oldSet {
		if !newSet[edge] {
			removed = append(removed, edge)
		}
	}
	for edge := range newSet {
		if !oldSet[edge] {
			added = append(added, edge)
		}
	}
	return added, removed
}

func relSet(rels map[string][]string) map[relEdge]bool {
	s := make(map[relEdge]bool)
	for rel, targets := range rels {
		for _, t := range targets {
			s[relEdge{rel, t}] = true
		}
	}
	return s
}

func changedAttrs(old, new map[string]any) []string {
	seen := make(map[string]bool)
	for k := range old {
		seen[k] = true
	}
	for k := range new {
		seen[k] = true
	}
	var changed []string
	for k := range seen {
		ov, ok1 := old[k]
		nv, ok2 := new[k]
		if !ok1 || !ok2 || fmt.Sprintf("%v", ov) != fmt.Sprintf("%v", nv) {
			changed = append(changed, k)
		}
	}
	sort.Strings(changed)
	return changed
}

func mapsEqual(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		bv, ok := b[k]
		if !ok || fmt.Sprintf("%v", av) != fmt.Sprintf("%v", bv) {
			return false
		}
	}
	return true
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
