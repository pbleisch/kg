package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Graph is the whole graph held in memory, built by walking the entity files.
//
// It replaces the persistent index kg used to keep. There is no cache to
// invalidate, nothing to sync, and nothing to keep warm: the files are the
// graph (P1), and at the corpus ceiling ADR-0026 states, reading them all is
// cheap enough that a resident process has nothing left to amortize (P4).
//
// Build one only for the queries that genuinely need every entity — reverse
// lookups, stats, whole-graph traversal. A single-entity read is ReadEntity
// and a typed find is ListEntitiesByType; neither should pay for a full walk.
type Graph struct {
	byID   map[string]*Entity
	byType map[string][]string
	// incoming maps a target id to the (source, relation) pairs pointing at
	// it. Built during the walk because that is the only pass over the files.
	incoming map[string][]Referrer
	// Warnings names files that could not be read or parsed.
	//
	// Deprecated: a file that cannot be read or parsed is now a structural
	// refusal and LoadGraph returns a *LoadError instead of building a graph
	// (ADR-0028). The field is kept so that a caller that printed it still
	// compiles; it is always empty.
	Warnings []string
}

// Referrer is one inbound edge.
type Referrer struct {
	ID       string
	Relation string
}

// LoadGraph walks <kgDir>/entities and builds the graph in memory.
//
// It is a load, so P3's enforced half binds: a file whose declared identity
// disagrees with its location, an identity two files claim, an illegal
// attribute name or a file that will not parse refuses the whole graph, and
// the returned *LoadError names every offending file (ADR-0028). Nothing here
// stops at the first — a corpus is repaired in one pass or not at all.
//
// A dangling relation target does not refuse. It is reported: see
// [Graph.Dangling].
func LoadGraph(kgDir string) (*Graph, error) {
	g := &Graph{
		byID:     make(map[string]*Entity),
		byType:   make(map[string][]string),
		incoming: make(map[string][]Referrer),
	}
	var errs []Structural
	paths := make(map[string][]string)

	entitiesDir := filepath.Join(kgDir, "entities")
	err := filepath.WalkDir(entitiesDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) && path == entitiesDir {
				return nil // a graph with no entities yet
			}
			errs = append(errs, Structural{
				Code: "unreadable", Files: []string{path},
				Message: fmt.Sprintf("cannot be walked: %v", walkErr),
			})
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(path, ".yaml") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			errs = append(errs, Structural{
				Code: "unreadable", Files: []string{path},
				Message: fmt.Sprintf("cannot be read: %v", err),
			})
			return nil
		}
		e, derr := decodeEntity(kgDir, path, data)
		if derr != nil {
			if ss, ok := LoadRefused(derr); ok {
				errs = append(errs, ss...)
				return nil
			}
			return derr
		}
		paths[e.ID] = append(paths[e.ID], path)
		g.add(e)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking entities: %w", err)
	}
	errs = append(errs, Collisions(paths)...)
	if len(errs) > 0 {
		SortStructural(errs)
		return nil, &LoadError{Errors: errs}
	}

	for t := range g.byType {
		sort.Strings(g.byType[t])
	}
	for id := range g.incoming {
		refs := g.incoming[id]
		sort.Slice(refs, func(i, j int) bool {
			if refs[i].ID != refs[j].ID {
				return refs[i].ID < refs[j].ID
			}
			return refs[i].Relation < refs[j].Relation
		})
	}
	return g, nil
}

func (g *Graph) add(e *Entity) {
	g.byID[e.ID] = e
	g.byType[e.Type] = append(g.byType[e.Type], e.ID)
	for rel, targets := range e.Relations {
		for _, t := range targets {
			g.incoming[t] = append(g.incoming[t], Referrer{ID: e.ID, Relation: rel})
		}
	}
}

// Len is the number of entities loaded.
func (g *Graph) Len() int { return len(g.byID) }

// Entity returns one entity by id.
func (g *Graph) Entity(id string) (*Entity, bool) {
	e, ok := g.byID[id]
	return e, ok
}

// Entities returns every entity, ordered by id.
func (g *Graph) Entities() []*Entity {
	ids := make([]string, 0, len(g.byID))
	for id := range g.byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]*Entity, len(ids))
	for i, id := range ids {
		out[i] = g.byID[id]
	}
	return out
}

// Types returns every type present, sorted.
func (g *Graph) Types() []string {
	ts := make([]string, 0, len(g.byType))
	for t := range g.byType {
		ts = append(ts, t)
	}
	sort.Strings(ts)
	return ts
}

// IDsByType returns the ids of one type, sorted.
func (g *Graph) IDsByType(typeName string) []string {
	return g.byType[typeName]
}

// ReferrersVia returns the ids pointing at targetID through one relation.
func (g *Graph) ReferrersVia(relation, targetID string) []string {
	var out []string
	for _, r := range g.incoming[targetID] {
		if r.Relation == relation {
			out = append(out, r.ID)
		}
	}
	return out
}

// Referrers returns the ids pointing at id through any relation, deduplicated
// and sorted.
func (g *Graph) Referrers(id string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, r := range g.incoming[id] {
		if _, ok := seen[r.ID]; ok {
			continue
		}
		seen[r.ID] = struct{}{}
		out = append(out, r.ID)
	}
	sort.Strings(out)
	return out
}

// A Dangling is one relation whose target is not in the graph.
type Dangling struct {
	From     string `json:"from"`
	Relation string `json:"relation"`
	To       string `json:"to"`
}

// Dangling returns every relation pointing at an entity the graph does not
// hold, ordered by source then relation then target.
//
// It is a report, not a refusal (ADR-0028): whether an edge resolves is a
// property of the corpus at one instant, not of the file holding the edge, and
// OKF §11 forbids a consumer rejecting a bundle over it. The write boundary
// refuses the same edge, because a producer asserting it is a different act.
func (g *Graph) Dangling() []Dangling {
	var out []Dangling
	for target, refs := range g.incoming {
		if _, ok := g.byID[target]; ok {
			continue
		}
		for _, r := range refs {
			out = append(out, Dangling{From: r.ID, Relation: r.Relation, To: target})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].From != out[j].From {
			return out[i].From < out[j].From
		}
		if out[i].Relation != out[j].Relation {
			return out[i].Relation < out[j].Relation
		}
		return out[i].To < out[j].To
	})
	return out
}

// Relations returns the targets of one relation on one entity.
func (g *Graph) Relations(id, relation string) []string {
	e, ok := g.byID[id]
	if !ok {
		return nil
	}
	return e.Relations[relation]
}

// FindEntities returns entities of one type matching the query options,
// applying filters, pagination and the relation/embedding projections.
//
// The returned entities are copies: callers strip embeddings and relations
// from them, and the graph must not be mutated by a read.
func (g *Graph) FindEntities(typeName string, opts QueryOptions) ([]*Entity, error) {
	var results []*Entity
	matched := 0
	for _, id := range g.byType[typeName] {
		e := g.byID[id]
		if !MatchFilters(e, opts.Filters) {
			continue
		}
		if matched < opts.Offset {
			matched++
			continue
		}
		results = append(results, project(e, opts))
		matched++
		if opts.Limit > 0 && len(results) >= opts.Limit {
			break
		}
	}
	return results, nil
}

// project copies an entity, applying the option's projections.
func project(e *Entity, opts QueryOptions) *Entity {
	out := &Entity{ID: e.ID, Type: e.Type}
	if len(e.Attributes) > 0 {
		out.Attributes = make(map[string]any, len(e.Attributes))
		for k, v := range e.Attributes {
			if !opts.IncludeEmbeddings && strings.HasSuffix(k, "_embedding") {
				continue
			}
			out.Attributes[k] = v
		}
	}
	if opts.IncludeRelations {
		out.Relations = e.Relations
	}
	return out
}

// Stats computes the graph-level metrics `kg stats` reports.
func (g *Graph) Stats() *GraphStats {
	s := &GraphStats{
		TypeCounts: make(map[string]int),
		RelCounts:  make(map[string]int),
	}
	linked := make(map[string]bool)
	for _, e := range g.byID {
		s.TotalEntities++
		s.TypeCounts[e.Type]++
		for rel, targets := range e.Relations {
			s.TotalLinks += len(targets)
			s.RelCounts[rel] += len(targets)
			if len(targets) > 0 {
				linked[e.ID] = true
			}
			for _, t := range targets {
				linked[t] = true
			}
		}
	}
	for id := range g.byID {
		if !linked[id] {
			s.Orphans++
		}
	}
	return s
}

// GraphStats holds graph-level metrics.
type GraphStats struct {
	TotalEntities int            `json:"total_entities"`
	TotalLinks    int            `json:"total_links"`
	TypeCounts    map[string]int `json:"type_counts"`
	RelCounts     map[string]int `json:"relation_counts"`
	Orphans       int            `json:"orphans"`
}
