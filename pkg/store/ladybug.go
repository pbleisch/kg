//go:build ladybug

package store

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"

	lbug "github.com/LadybugDB/go-ladybug"
)

const ladybugAvailable = true

// LadybugAvailable reports whether this binary was built with LadybugDB support.
func LadybugAvailable() bool { return true }

type ladybugIndex struct {
	db   *lbug.Database
	conn *lbug.Connection
}

func openLadybug(kgDir string) (Indexer, error) {
	dbDir := filepath.Join(kgDir, ".cache", "ladybug")
	// Only ensure the parent (.cache/) exists — LadybugDB must create
	// the database directory itself. Pre-creating it causes OpenDatabase
	// to fail with status 1 on an empty directory.
	if err := os.MkdirAll(filepath.Dir(dbDir), 0o755); err != nil {
		return nil, fmt.Errorf("creating cache dir: %w", err)
	}

	cfg := lbug.DefaultSystemConfig()
	db, err := lbug.OpenDatabase(dbDir, cfg)
	if err != nil {
		return nil, fmt.Errorf("open ladybug: %w", err)
	}

	conn, err := lbug.OpenConnection(db)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("open connection: %w", err)
	}

	// Initialize schema: a generic Entity node table and a HasRel edge table.
	for _, q := range []string{
		"CREATE NODE TABLE IF NOT EXISTS Entity(id STRING, type STRING, attrs STRING, PRIMARY KEY(id))",
		"CREATE REL TABLE IF NOT EXISTS HasRel(FROM Entity TO Entity, name STRING)",
	} {
		result, err := conn.Query(q)
		if err != nil {
			conn.Close()
			db.Close()
			return nil, fmt.Errorf("init schema: %w", err)
		}
		result.Close()
	}

	return &ladybugIndex{db: db, conn: conn}, nil
}

func (idx *ladybugIndex) Close() error {
	idx.conn.Close()
	idx.db.Close()
	return nil
}

func (idx *ladybugIndex) AddEntity(e *Entity) error {
	attrsJSON, err := json.Marshal(e.Attributes)
	if err != nil {
		return fmt.Errorf("marshal attrs: %w", err)
	}

	// Upsert the entity node.
	stmt, err := idx.conn.Prepare(
		"MERGE (n:Entity {id: $id}) SET n.type = $type, n.attrs = $attrs",
	)
	if err != nil {
		return fmt.Errorf("prepare merge: %w", err)
	}
	defer stmt.Close()

	result, err := idx.conn.Execute(stmt, map[string]any{
		"id":    e.ID,
		"type":  e.Type,
		"attrs": string(attrsJSON),
	})
	if err != nil {
		return fmt.Errorf("merge entity: %w", err)
	}
	result.Close()

	// Remove old outgoing relationships before adding current ones.
	delStmt, err := idx.conn.Prepare(
		"MATCH (n:Entity {id: $id})-[r:HasRel]->() DELETE r",
	)
	if err != nil {
		return fmt.Errorf("prepare delete rels: %w", err)
	}
	defer delStmt.Close()

	result, err = idx.conn.Execute(delStmt, map[string]any{"id": e.ID})
	if err != nil {
		return fmt.Errorf("delete old rels: %w", err)
	}
	result.Close()

	// Create new relationship edges, merging stub targets as needed.
	if len(e.Relations) > 0 {
		relStmt, err := idx.conn.Prepare(
			"MATCH (a:Entity {id: $from}) " +
				"MERGE (b:Entity {id: $to}) " +
				"CREATE (a)-[:HasRel {name: $name}]->(b)",
		)
		if err != nil {
			return fmt.Errorf("prepare create rel: %w", err)
		}
		defer relStmt.Close()

		for rel, targets := range e.Relations {
			for _, t := range targets {
				result, err = idx.conn.Execute(relStmt, map[string]any{
					"from": e.ID,
					"to":   t,
					"name": rel,
				})
				if err != nil {
					return fmt.Errorf("create rel %s->%s: %w", rel, t, err)
				}
				result.Close()
			}
		}
	}

	return nil
}

// RemoveEntity deletes outgoing edges and clears the node to a stub.
// Incoming edges from other entities are preserved (matching BoltDB behavior).
func (idx *ladybugIndex) RemoveEntity(id string) error {
	// Delete outgoing relationships.
	delStmt, err := idx.conn.Prepare(
		"MATCH (n:Entity {id: $id})-[r:HasRel]->() DELETE r",
	)
	if err != nil {
		return fmt.Errorf("prepare delete rels: %w", err)
	}
	defer delStmt.Close()

	result, err := idx.conn.Execute(delStmt, map[string]any{"id": id})
	if err != nil {
		return fmt.Errorf("delete rels: %w", err)
	}
	result.Close()

	// Clear node to stub (preserves incoming edges).
	clearStmt, err := idx.conn.Prepare(
		"MATCH (n:Entity {id: $id}) SET n.type = '', n.attrs = '{}'",
	)
	if err != nil {
		return fmt.Errorf("prepare clear: %w", err)
	}
	defer clearStmt.Close()

	result, err = idx.conn.Execute(clearStmt, map[string]any{"id": id})
	if err != nil {
		return fmt.Errorf("clear entity: %w", err)
	}
	result.Close()

	return nil
}

func (idx *ladybugIndex) GetEntity(id string) (*Entity, error) {
	stmt, err := idx.conn.Prepare(
		"MATCH (n:Entity {id: $id}) WHERE n.type <> '' RETURN n.type, n.attrs",
	)
	if err != nil {
		return nil, fmt.Errorf("prepare get: %w", err)
	}
	defer stmt.Close()

	result, err := idx.conn.Execute(stmt, map[string]any{"id": id})
	if err != nil {
		return nil, fmt.Errorf("get entity: %w", err)
	}
	defer result.Close()

	if !result.HasNext() {
		return nil, fmt.Errorf("entity not found: %s", id)
	}

	tuple, err := result.Next()
	if err != nil {
		return nil, fmt.Errorf("read result: %w", err)
	}

	typeVal, err := tuple.GetValue(0)
	if err != nil {
		return nil, fmt.Errorf("get type: %w", err)
	}
	attrsVal, err := tuple.GetValue(1)
	if err != nil {
		return nil, fmt.Errorf("get attrs: %w", err)
	}
	tuple.Close()

	e := &Entity{
		ID:         id,
		Type:       fmt.Sprint(typeVal),
		Attributes: make(map[string]any),
		Relations:  make(map[string][]string),
	}

	if attrsStr, ok := attrsVal.(string); ok && attrsStr != "" && attrsStr != "{}" {
		if err := json.Unmarshal([]byte(attrsStr), &e.Attributes); err != nil {
			return nil, fmt.Errorf("unmarshal attrs: %w", err)
		}
	}

	// Fetch outgoing relationships.
	relStmt, err := idx.conn.Prepare(
		"MATCH (n:Entity {id: $id})-[r:HasRel]->(t:Entity) RETURN r.name, t.id",
	)
	if err != nil {
		return nil, fmt.Errorf("prepare get rels: %w", err)
	}
	defer relStmt.Close()

	relResult, err := idx.conn.Execute(relStmt, map[string]any{"id": id})
	if err != nil {
		return nil, fmt.Errorf("get rels: %w", err)
	}
	defer relResult.Close()

	for relResult.HasNext() {
		tuple, err := relResult.Next()
		if err != nil {
			return nil, fmt.Errorf("read rel: %w", err)
		}
		relName, _ := tuple.GetValue(0)
		targetID, _ := tuple.GetValue(1)
		tuple.Close()

		name := fmt.Sprint(relName)
		target := fmt.Sprint(targetID)
		e.Relations[name] = append(e.Relations[name], target)
	}

	return e, nil
}

func (idx *ladybugIndex) FindByType(typeName string) ([]string, error) {
	stmt, err := idx.conn.Prepare(
		"MATCH (n:Entity) WHERE n.type = $type RETURN n.id",
	)
	if err != nil {
		return nil, fmt.Errorf("prepare find: %w", err)
	}
	defer stmt.Close()

	result, err := idx.conn.Execute(stmt, map[string]any{"type": typeName})
	if err != nil {
		return nil, fmt.Errorf("find by type: %w", err)
	}
	defer result.Close()

	var ids []string
	for result.HasNext() {
		tuple, err := result.Next()
		if err != nil {
			return nil, fmt.Errorf("read result: %w", err)
		}
		val, _ := tuple.GetValue(0)
		tuple.Close()
		ids = append(ids, fmt.Sprint(val))
	}
	return ids, nil
}

func (idx *ladybugIndex) FindEntities(typeName string, opts QueryOptions) ([]*Entity, error) {
	// When no attribute filters, push SKIP/LIMIT into Cypher so the DB
	// engine avoids materializing all rows.
	canPushPagination := len(opts.Filters) == 0

	cypher := "MATCH (n:Entity) WHERE n.type = $type RETURN n.id, n.attrs"
	params := map[string]any{"type": typeName}
	if canPushPagination {
		if opts.Offset > 0 {
			cypher += fmt.Sprintf(" SKIP %d", opts.Offset)
		}
		if opts.Limit > 0 {
			cypher += fmt.Sprintf(" LIMIT %d", opts.Limit)
		}
	}

	stmt, err := idx.conn.Prepare(cypher)
	if err != nil {
		return DefaultFindEntities(idx, typeName, opts)
	}
	defer stmt.Close()

	qResult, err := idx.conn.Execute(stmt, params)
	if err != nil {
		return DefaultFindEntities(idx, typeName, opts)
	}
	defer qResult.Close()

	var results []*Entity
	matched := 0
	for qResult.HasNext() {
		tuple, err := qResult.Next()
		if err != nil {
			continue
		}
		idVal, _ := tuple.GetValue(0)
		attrsVal, _ := tuple.GetValue(1)
		tuple.Close()

		id := fmt.Sprint(idVal)
		attrs := make(map[string]any)
		if attrsStr, ok := attrsVal.(string); ok && attrsStr != "" && attrsStr != "{}" {
			if err := json.Unmarshal([]byte(attrsStr), &attrs); err != nil {
				continue
			}
		}

		e := &Entity{
			ID:         id,
			Type:       typeName,
			Attributes: attrs,
		}

		// Client-side filter + pagination when filters prevent Cypher push-down
		if !canPushPagination {
			if !MatchFilters(e, opts.Filters) {
				continue
			}
			if matched < opts.Offset {
				matched++
				continue
			}
		}

		// Fetch relations for this entity
		e.Relations = make(map[string][]string)
		relStmt, err := idx.conn.Prepare(
			"MATCH (n:Entity {id: $id})-[r:HasRel]->(t:Entity) RETURN r.name, t.id",
		)
		if err == nil {
			relResult, err := idx.conn.Execute(relStmt, map[string]any{"id": id})
			if err == nil {
				for relResult.HasNext() {
					rt, err := relResult.Next()
					if err != nil {
						break
					}
					relName, _ := rt.GetValue(0)
					targetID, _ := rt.GetValue(1)
					rt.Close()
					e.Relations[fmt.Sprint(relName)] = append(e.Relations[fmt.Sprint(relName)], fmt.Sprint(targetID))
				}
				relResult.Close()
			}
			relStmt.Close()
		}

		if !opts.IncludeEmbeddings {
			StripEmbeddings(e)
		}
		results = append(results, e)
		matched++
		if !canPushPagination && opts.Limit > 0 && len(results) >= opts.Limit {
			break
		}
	}
	return results, nil
}

// SimilarEntities implements VectorSearcher. It loads all entities of the
// given type in a single bulk query, extracts vectors from the specified
// fields (falling back to <field>_embedding if the bare name is absent),
// computes similarity with the query vector, and returns the top-N results
// sorted by score.
func (idx *ladybugIndex) SimilarEntities(params SimilarityParams) ([]SimilarityResult, error) {
	// Bulk query: fetch id + attrs for all entities of the type in one call.
	stmt, err := idx.conn.Prepare(
		"MATCH (n:Entity) WHERE n.type = $type AND n.attrs <> '{}' RETURN n.id, n.attrs",
	)
	if err != nil {
		return nil, fmt.Errorf("prepare bulk similar: %w", err)
	}
	defer stmt.Close()

	qResult, err := idx.conn.Execute(stmt, map[string]any{"type": params.TypeName})
	if err != nil {
		return nil, fmt.Errorf("bulk similar query: %w", err)
	}
	defer qResult.Close()

	var results []SimilarityResult
	for qResult.HasNext() {
		tuple, err := qResult.Next()
		if err != nil {
			continue
		}
		idVal, _ := tuple.GetValue(0)
		attrsVal, _ := tuple.GetValue(1)
		tuple.Close()

		id := fmt.Sprint(idVal)
		attrsStr, ok := attrsVal.(string)
		if !ok || attrsStr == "" || attrsStr == "{}" {
			continue
		}

		attrs := make(map[string]any)
		if err := json.Unmarshal([]byte(attrsStr), &attrs); err != nil {
			continue
		}

		bestScore := math.Inf(-1)
		bestField := ""

		for _, field := range params.Fields {
			resolvedField, vec := ResolveVector(attrs, field)
			if vec == nil {
				continue
			}

			score, err := computeScore(params.Metric, params.Vector, vec)
			if err != nil {
				continue
			}

			if score > bestScore {
				bestScore = score
				bestField = resolvedField
			}
		}

		if math.IsInf(bestScore, -1) {
			continue
		}

		// Build minimal entity for result (avoid full GetEntity query).
		e := &Entity{
			ID:         id,
			Type:       params.TypeName,
			Attributes: attrs,
		}

		results = append(results, SimilarityResult{
			Entity:     e,
			Score:      bestScore,
			MatchField: bestField,
		})
	}

	SortResults(results)
	if params.TopN > 0 && len(results) > params.TopN {
		results = results[:params.TopN]
	}
	return results, nil
}

func (idx *ladybugIndex) FindByRelationTarget(relation, targetID string) ([]string, error) {
	stmt, err := idx.conn.Prepare(
		"MATCH (n:Entity)-[r:HasRel]->(t:Entity {id: $target}) " +
			"WHERE r.name = $rel RETURN n.id",
	)
	if err != nil {
		return nil, fmt.Errorf("prepare reverse: %w", err)
	}
	defer stmt.Close()

	result, err := idx.conn.Execute(stmt, map[string]any{
		"target": targetID,
		"rel":    relation,
	})
	if err != nil {
		return nil, fmt.Errorf("reverse lookup: %w", err)
	}
	defer result.Close()

	var ids []string
	for result.HasNext() {
		tuple, err := result.Next()
		if err != nil {
			return nil, fmt.Errorf("read result: %w", err)
		}
		val, _ := tuple.GetValue(0)
		tuple.Close()
		ids = append(ids, fmt.Sprint(val))
	}
	return ids, nil
}

// FindReferrers returns unique entity IDs that point to the given entity via
// any relation.
func (idx *ladybugIndex) FindReferrers(id string) ([]string, error) {
	stmt, err := idx.conn.Prepare(
		"MATCH (n:Entity)-[r:HasRel]->(t:Entity {id: $target}) " +
			"RETURN DISTINCT n.id",
	)
	if err != nil {
		return nil, fmt.Errorf("prepare referrers: %w", err)
	}
	defer stmt.Close()

	result, err := idx.conn.Execute(stmt, map[string]any{"target": id})
	if err != nil {
		return nil, fmt.Errorf("find referrers: %w", err)
	}
	defer result.Close()

	var ids []string
	for result.HasNext() {
		tuple, err := result.Next()
		if err != nil {
			return nil, fmt.Errorf("read result: %w", err)
		}
		val, _ := tuple.GetValue(0)
		tuple.Close()
		ids = append(ids, fmt.Sprint(val))
	}
	return ids, nil
}

// UpsertEntity delegates to AddEntity which already uses MERGE (upsert).
func (idx *ladybugIndex) UpsertEntity(e *Entity) error {
	return idx.AddEntity(e)
}

// IndexStats computes graph statistics via Cypher aggregation queries.
func (idx *ladybugIndex) IndexStats() (*IndexStats, error) {
	stats := &IndexStats{
		TypeCounts: make(map[string]int),
		RelCounts:  make(map[string]int),
	}

	// Count entities by type.
	result, err := idx.conn.Query(
		"MATCH (n:Entity) WHERE n.type <> '' RETURN n.type, count(*)")
	if err != nil {
		return nil, fmt.Errorf("count types: %w", err)
	}
	for result.HasNext() {
		tuple, err := result.Next()
		if err != nil {
			result.Close()
			return nil, err
		}
		typeName, _ := tuple.GetValue(0)
		count, _ := tuple.GetValue(1)
		tuple.Close()
		name := fmt.Sprint(typeName)
		c := toInt(count)
		stats.TypeCounts[name] = c
		stats.TotalEntities += c
	}
	result.Close()

	// Count relations by name.
	result, err = idx.conn.Query(
		"MATCH ()-[r:HasRel]->() RETURN r.name, count(*)")
	if err != nil {
		return nil, fmt.Errorf("count rels: %w", err)
	}
	for result.HasNext() {
		tuple, err := result.Next()
		if err != nil {
			result.Close()
			return nil, err
		}
		relName, _ := tuple.GetValue(0)
		count, _ := tuple.GetValue(1)
		tuple.Close()
		name := fmt.Sprint(relName)
		c := toInt(count)
		stats.RelCounts[name] = c
		stats.TotalLinks += c
	}
	result.Close()

	// Count orphans: entities with no outgoing or incoming relations.
	result, err = idx.conn.Query(
		"MATCH (n:Entity) WHERE n.type <> '' AND NOT EXISTS { MATCH (n)-[:HasRel]-() } RETURN count(*)")
	if err != nil {
		return nil, fmt.Errorf("count orphans: %w", err)
	}
	if result.HasNext() {
		tuple, err := result.Next()
		if err == nil {
			val, _ := tuple.GetValue(0)
			stats.Orphans = toInt(val)
			tuple.Close()
		}
	}
	result.Close()

	return stats, nil
}

func toInt(v any) int {
	switch n := v.(type) {
	case int64:
		return int(n)
	case int:
		return n
	case float64:
		return int(n)
	default:
		return 0
	}
}
