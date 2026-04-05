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

// SimilarEntities implements VectorSearcher. It loads all entities of the
// given type, extracts vectors from the specified fields (falling back to
// <field>_embedding if the bare name is absent), computes similarity with
// the query vector, and returns the top-N results sorted by score.
func (idx *ladybugIndex) SimilarEntities(params SimilarityParams) ([]SimilarityResult, error) {
	ids, err := idx.FindByType(params.TypeName)
	if err != nil {
		return nil, fmt.Errorf("finding type %s: %w", params.TypeName, err)
	}

	var results []SimilarityResult
	for _, id := range ids {
		e, err := idx.GetEntity(id)
		if err != nil {
			continue
		}

		bestScore := math.Inf(-1)
		bestField := ""

		for _, field := range params.Fields {
			// Resolve field: try exact name, then <name>_embedding.
			resolvedField := field
			val, ok := e.Attributes[field]
			if !ok {
				resolvedField = field + "_embedding"
				val, ok = e.Attributes[resolvedField]
				if !ok {
					continue
				}
			}

			vec, ok := ExtractVector(val)
			if !ok {
				continue
			}

			score, err := computeScore(params.Metric, params.Vector, vec)
			if err != nil {
				continue // dimension mismatch — skip field
			}

			if score > bestScore {
				bestScore = score
				bestField = resolvedField
			}
		}

		if math.IsInf(bestScore, -1) {
			continue // no usable vector field on this entity
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
