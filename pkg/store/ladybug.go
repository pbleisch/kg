package store

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	lbug "github.com/LadybugDB/go-ladybug"
)

type ladybugIndex struct {
	db   *lbug.Database
	conn *lbug.Connection

	// HNSW vector index state.
	vecExt  bool                   // true if vector extension loaded successfully
	embDim  int                    // embedding dimension (0 = not yet detected)
	embStmt *lbug.PreparedStatement // cached MERGE statement for EmbVec upserts
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

	idx := &ladybugIndex{db: db, conn: conn}
	idx.initVectorExtension()
	return idx, nil
}

// initVectorExtension loads the vector extension and probes for an
// existing EmbVec table (from a prior sync). Non-fatal on failure —
// the index falls back to brute-force similarity search.
func (idx *ladybugIndex) initVectorExtension() {
	for _, q := range []string{"INSTALL vector", "LOAD EXTENSION vector"} {
		r, err := idx.conn.Query(q)
		if err != nil {
			return // extension unavailable — brute-force fallback
		}
		r.Close()
	}
	idx.vecExt = true

	// Probe for existing EmbVec table to recover embDim.
	r, err := idx.conn.Query(
		"MATCH (v:EmbVec) RETURN v.vec LIMIT 1",
	)
	if err != nil {
		return // table doesn't exist yet — will be created lazily
	}
	defer r.Close()
	if r.HasNext() {
		tuple, err := r.Next()
		if err == nil {
			if vecVal, err := tuple.GetValue(0); err == nil {
				if arr, ok := vecVal.([]any); ok {
					idx.embDim = len(arr)
				}
			}
			tuple.Close()
		}
	}
}

// ensureEmbVec creates the EmbVec sidecar table and HNSW index on first use.
// The table stores embedding vectors extracted from entity attributes so
// SimilarEntities can use approximate nearest-neighbor search instead of
// brute-force scanning.
func (idx *ladybugIndex) ensureEmbVec(dim int) error {
	if idx.embDim > 0 {
		return nil // already initialized
	}
	if !idx.vecExt {
		return fmt.Errorf("vector extension not available")
	}

	// EmbVec primary key = "entityID:field" (e.g., "flow_state:description_embedding").
	createQ := fmt.Sprintf(
		"CREATE NODE TABLE IF NOT EXISTS EmbVec(id STRING, entity_id STRING, entity_type STRING, field STRING, vec FLOAT[%d], PRIMARY KEY(id))",
		dim,
	)
	r, err := idx.conn.Query(createQ)
	if err != nil {
		return fmt.Errorf("create EmbVec table: %w", err)
	}
	r.Close()

	// Create HNSW index for approximate nearest-neighbor queries.
	r, err = idx.conn.Query(
		"CALL CREATE_VECTOR_INDEX('EmbVec', 'emb_hnsw', 'vec', metric := 'cosine')",
	)
	if err != nil {
		// Index might already exist from a previous run — that's fine.
		if !strings.Contains(err.Error(), "already exists") {
			return fmt.Errorf("create HNSW index: %w", err)
		}
	} else {
		r.Close()
	}

	idx.embDim = dim

	// Pre-prepare the CREATE statement for embedding inserts.
	// HNSW-indexed columns cannot be updated via SET, so we use
	// delete-then-create in upsertEmbeddings.
	idx.embStmt, err = idx.conn.Prepare(
		"CREATE (v:EmbVec {id: $id, entity_id: $eid, entity_type: $etype, field: $field, vec: $vec})",
	)
	if err != nil {
		// Non-fatal: upsertEmbeddings will skip if embStmt is nil.
		idx.embStmt = nil
	}

	return nil
}

func (idx *ladybugIndex) Close() error {
	if idx.embStmt != nil {
		idx.embStmt.Close()
	}
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

	// Upsert embedding vectors into the HNSW-indexed sidecar table.
	idx.upsertEmbeddings(e)

	return nil
}

// upsertEmbeddings extracts *_embedding fields from entity attributes and
// writes them to the EmbVec sidecar table for HNSW-accelerated search.
// Uses delete-then-create because HNSW-indexed columns cannot be updated
// in-place (KuzuDB limitation). Best-effort: failures are silently ignored.
func (idx *ladybugIndex) upsertEmbeddings(e *Entity) {
	if !idx.vecExt {
		return
	}

	// Collect embedding fields.
	type embEntry struct {
		field string
		vec   []float64
	}
	var entries []embEntry
	for k, v := range e.Attributes {
		if !strings.HasSuffix(k, "_embedding") {
			continue
		}
		vec, ok := ExtractVector(v)
		if !ok || len(vec) == 0 {
			continue
		}
		entries = append(entries, embEntry{field: k, vec: vec})
	}
	if len(entries) == 0 {
		return
	}

	// Lazily create the EmbVec table on first embedding encounter.
	if idx.embDim == 0 {
		if err := idx.ensureEmbVec(len(entries[0].vec)); err != nil {
			return
		}
	}

	if idx.embStmt == nil {
		return
	}

	// Delete existing embeddings for this entity before re-inserting.
	idx.removeEmbeddings(e.ID)

	for _, entry := range entries {
		if len(entry.vec) != idx.embDim {
			continue // dimension mismatch — skip
		}
		compositeID := e.ID + ":" + entry.field
		// Convert []float64 to []any for the driver.
		vecAny := make([]any, len(entry.vec))
		for i, f := range entry.vec {
			vecAny[i] = f
		}
		r, err := idx.conn.Execute(idx.embStmt, map[string]any{
			"id":    compositeID,
			"eid":   e.ID,
			"etype": e.Type,
			"field": entry.field,
			"vec":   vecAny,
		})
		if err != nil {
			continue
		}
		r.Close()
	}
}

// removeEmbeddings deletes all EmbVec rows for the given entity ID.
func (idx *ladybugIndex) removeEmbeddings(id string) {
	if !idx.vecExt || idx.embDim == 0 {
		return
	}
	// Delete by entity_id (may match multiple fields).
	stmt, err := idx.conn.Prepare(
		"MATCH (v:EmbVec) WHERE v.entity_id = $eid DELETE v",
	)
	if err != nil {
		return
	}
	defer stmt.Close()
	r, err := idx.conn.Execute(stmt, map[string]any{"eid": id})
	if err != nil {
		return
	}
	r.Close()
}

// RemoveEntity deletes outgoing edges, clears the node to a stub, and
// removes any associated embedding vectors from the HNSW sidecar table.
// Incoming edges from other entities are preserved.
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

	// Remove embedding vectors from sidecar table (best-effort).
	idx.removeEmbeddings(id)

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

	// Prepare the relation query once if relations are requested.
	var relStmt *lbug.PreparedStatement
	if opts.IncludeRelations {
		relStmt, err = idx.conn.Prepare(
			"MATCH (n:Entity {id: $id})-[r:HasRel]->(t:Entity) RETURN r.name, t.id",
		)
		if err != nil {
			relStmt = nil
		} else {
			defer relStmt.Close()
		}
	}

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

		if !canPushPagination {
			if !MatchFilters(e, opts.Filters) {
				continue
			}
			if matched < opts.Offset {
				matched++
				continue
			}
		}

		// Fetch relations using the pre-prepared statement.
		if relStmt != nil {
			e.Relations = make(map[string][]string)
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
					name := fmt.Sprint(relName)
					e.Relations[name] = append(e.Relations[name], fmt.Sprint(targetID))
				}
				relResult.Close()
			}
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
	// Try HNSW-accelerated search first.
	if idx.embDim > 0 && len(params.Vector) == idx.embDim {
		if results, err := idx.similarHNSW(params); err == nil {
			return results, nil
		}
		// Fall through to brute-force on HNSW failure.
	}

	return idx.similarBruteForce(params)
}

// similarHNSW uses the HNSW vector index for approximate nearest-neighbor
// search. Over-queries by a factor to compensate for post-filtering by
// entity_type and field, then refines with exact cosine similarity.
func (idx *ladybugIndex) similarHNSW(params SimilarityParams) ([]SimilarityResult, error) {
	// Over-query to account for type/field filtering.
	k := params.TopN
	if k <= 0 {
		k = 100
	}
	fetchK := k * 20
	if fetchK < 200 {
		fetchK = 200
	}

	// Build the field filter set for post-filtering.
	fieldSet := make(map[string]bool, len(params.Fields)*2)
	for _, f := range params.Fields {
		fieldSet[f] = true
		fieldSet[f+"_embedding"] = true
	}

	vecAny := make([]any, len(params.Vector))
	for i, f := range params.Vector {
		vecAny[i] = f
	}

	// QUERY_VECTOR_INDEX returns (node, distance) where distance is
	// cosine distance (1 - similarity) for metric='cosine'.
	q := fmt.Sprintf(
		"CALL QUERY_VECTOR_INDEX('EmbVec', 'emb_hnsw', $vec, %d) RETURN node.entity_id, node.entity_type, node.field, distance",
		fetchK,
	)
	stmt, err := idx.conn.Prepare(q)
	if err != nil {
		return nil, fmt.Errorf("prepare HNSW query: %w", err)
	}
	defer stmt.Close()

	result, err := idx.conn.Execute(stmt, map[string]any{"vec": vecAny})
	if err != nil {
		return nil, fmt.Errorf("HNSW query: %w", err)
	}
	defer result.Close()

	var results []SimilarityResult
	seen := make(map[string]bool)
	for result.HasNext() {
		tuple, err := result.Next()
		if err != nil {
			continue
		}
		eidVal, _ := tuple.GetValue(0)
		etypeVal, _ := tuple.GetValue(1)
		fieldVal, _ := tuple.GetValue(2)
		distVal, _ := tuple.GetValue(3)
		tuple.Close()

		entityID := fmt.Sprint(eidVal)
		entityType := fmt.Sprint(etypeVal)
		field := fmt.Sprint(fieldVal)

		// Post-filter by type and field.
		if entityType != params.TypeName {
			continue
		}
		if !fieldSet[field] {
			continue
		}
		if seen[entityID] {
			continue
		}
		seen[entityID] = true

		dist, ok := distVal.(float64)
		if !ok {
			continue
		}
		score := 1.0 - dist // cosine distance → cosine similarity

		// Build minimal entity.
		e := &Entity{
			ID:   entityID,
			Type: entityType,
		}

		results = append(results, SimilarityResult{
			Entity:     e,
			Score:      score,
			MatchField: field,
		})
	}

	SortResults(results)
	if params.TopN > 0 && len(results) > params.TopN {
		results = results[:params.TopN]
	}
	return results, nil
}

// similarBruteForce is the original O(M) scan used when HNSW is unavailable.
func (idx *ladybugIndex) similarBruteForce(params SimilarityParams) ([]SimilarityResult, error) {
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

// UpsertEntities batch-upserts multiple entities.
func (idx *ladybugIndex) UpsertEntities(entities []*Entity) error {
	for _, e := range entities {
		if err := idx.AddEntity(e); err != nil {
			return err
		}
	}
	return nil
}

// RemoveEntities batch-removes multiple entities by ID.
func (idx *ladybugIndex) RemoveEntities(ids []string) error {
	for _, id := range ids {
		if err := idx.RemoveEntity(id); err != nil {
			return err
		}
	}
	return nil
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
