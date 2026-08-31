package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
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
	// Warnings names files that could not be read or parsed. A malformed file
	// is skipped, not fatal: a graph under construction is legitimately
	// incomplete (P3), and the load half of P3 is increment 4's job.
	Warnings []string
}

// Referrer is one inbound edge.
type Referrer struct {
	ID       string
	Relation string
}

// LoadGraph walks <kgDir>/entities and builds the graph in memory.
func LoadGraph(kgDir string) (*Graph, error) {
	g := &Graph{
		byID:     make(map[string]*Entity),
		byType:   make(map[string][]string),
		incoming: make(map[string][]Referrer),
	}

	entitiesDir := filepath.Join(kgDir, "entities")
	err := filepath.WalkDir(entitiesDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) && path == entitiesDir {
				return nil // a graph with no entities yet
			}
			g.Warnings = append(g.Warnings, fmt.Sprintf("%s: %v", path, walkErr))
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(path, ".yaml") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			g.Warnings = append(g.Warnings, fmt.Sprintf("%s: read: %v", path, err))
			return nil
		}
		var e Entity
		if err := yaml.Unmarshal(data, &e); err != nil {
			g.Warnings = append(g.Warnings, fmt.Sprintf("%s: parse: %v", path, err))
			return nil
		}
		g.add(&e)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking entities: %w", err)
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
