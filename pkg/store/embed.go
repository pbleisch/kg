package store

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pbleisch/kg/pkg/vector"
)

// EmbedInput is the JSON payload sent to the external embedding command.
type EmbedInput struct {
	Text     string `json:"text"`
	Field    string `json:"field"`
	EntityID string `json:"entity_id"`
	Type     string `json:"type"`
}

// EmbedResult collects statistics from an embed operation.
type EmbedResult struct {
	// Embedded counts vectors newly computed by the embedding command.
	Embedded int `json:"embedded"`
	// Carried counts vectors reused from the previous sidecar because the
	// text they were computed from has not changed.
	Carried int `json:"carried"`
	// Dropped counts sidecar rows discarded because the entity or the
	// attribute behind them is gone.
	Dropped int `json:"dropped"`
	Errors  int `json:"errors"`
	// Rows is how many vectors the sidecar actually holds. It is normally
	// Embedded+Carried, and is less when two entity files claim one id —
	// which is a corpus defect, not a rounding difference.
	Rows     int      `json:"rows"`
	Dim      int      `json:"dim,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// CacheDir names the derived-data directory for a graph. Everything under it
// is rebuildable by `kg sync` and nothing under it may be committed (P2).
func CacheDir(kgDir string) string { return filepath.Join(kgDir, ".cache") }

// TextChecksum returns the SHA-256 hex digest of a string value.
func TextChecksum(text string) string {
	h := sha256.Sum256([]byte(text))
	return fmt.Sprintf("%x", h)
}

// Embed brings the vector sidecar level with the graph.
//
// Vectors go to <kgDir>/.cache/vectors.bin, never into the entity files. That
// is the whole point of the change: an entity file holds what someone
// authored, and a 384-float vector is not that (P2). A re-embed after a model
// swap now rewrites one derived file instead of every entity in the corpus.
//
// Work is decided per (entity, field) by comparing the SHA-256 of the text
// against the checksum the sidecar recorded. Unchanged pairs are carried over
// from the old sidecar without re-running the embedding command; pairs whose
// entity or attribute has disappeared are dropped. Passing rebuild re-embeds
// everything, and changing embedders does so implicitly, because the sidecar
// records which embedder produced it.
//
// If entityIDs is non-empty only those entities are considered for
// re-embedding — but every other pair is still carried through, so the sidecar
// that lands is always complete.
func Embed(kgDir string, rebuild, dryRun bool, entityIDs ...string) (*EmbedResult, error) {
	emb, err := ResolveEmbedder(kgDir)
	if err != nil {
		return nil, err
	}
	return EmbedWith(kgDir, emb, rebuild, dryRun, entityIDs...)
}

// EmbedWith is Embed with the embedder supplied by the caller, so a caller
// that has already resolved one does not resolve it twice.
func EmbedWith(kgDir string, emb *Embedder, rebuild, dryRun bool, entityIDs ...string) (*EmbedResult, error) {
	if emb == nil {
		return nil, &ErrNoEmbedder{KgDir: kgDir}
	}
	cacheDir := CacheDir(kgDir)
	result := &EmbedResult{}

	// Only these entities may be re-embedded; everything else is carried.
	var only map[string]bool
	if len(entityIDs) > 0 {
		only = make(map[string]bool, len(entityIDs))
		for _, id := range entityIDs {
			only[id] = true
		}
	}

	// The previous sidecar, when one exists and is usable, supplies the
	// vectors for unchanged pairs. A stale or missing one is not an error:
	// it just means nothing can be carried.
	var prev *vector.Store
	if !rebuild {
		if s, err := vector.Open(cacheDir, emb.Digest()); err == nil {
			prev = s
			defer prev.Close()
		}
	}

	// Which entities to read. A named set with a sidecar to carry the rest
	// from reads only those entities — which is what makes a write cost the
	// entities it touched rather than the corpus. Anything else has to walk,
	// because only a walk can find what has changed or gone.
	var entities []*Entity
	var err error
	if only != nil && prev != nil {
		for id := range only {
			e, readErr := ReadEntity(kgDir, id)
			if readErr != nil {
				continue // deleted in the same batch; its rows are dropped below
			}
			entities = append(entities, e)
		}
		sort.Slice(entities, func(i, j int) bool { return entities[i].ID < entities[j].ID })
	} else {
		entities, err = ListEntities(kgDir)
		if err != nil {
			return nil, fmt.Errorf("listing entities: %w", err)
		}
	}

	type work struct {
		entry vector.Entry
		text  string
	}
	var (
		carry   []vector.Entry
		pending []work
	)

	for _, e := range entities {
		for _, field := range emb.Fields {
			val, ok := e.Attributes[field]
			if !ok {
				continue
			}
			// AttrText, not %v: a multi-valued field renders as
			// `[map[source:… text:…]]` under %v, and every vector built from
			// that string is built from Go's debug formatting rather than
			// from what anyone wrote.
			text := AttrText(val)
			if text == "" {
				continue
			}
			entry := vector.Entry{
				ID:       e.ID,
				Type:     e.Type,
				Field:    field,
				Checksum: TextChecksum(text),
			}
			var old vector.Entry
			hasOld := false
			if prev != nil {
				old, hasOld = prev.Entry(e.ID, field)
			}
			switch {
			case hasOld && old.Checksum == entry.Checksum:
				// The text has not changed, so neither has its vector.
				entry.Row = old.Row
				carry = append(carry, entry)
			case only != nil && !only[e.ID]:
				// Out of scope for this pass. Keep the vector so `similar`
				// still finds the entity, but keep the *old* checksum too, so
				// the next whole-graph pass sees it as stale and re-embeds it.
				if hasOld {
					entry.Checksum = old.Checksum
					entry.Row = old.Row
					carry = append(carry, entry)
				}
			default:
				pending = append(pending, work{entry: entry, text: text})
			}
		}
	}

	// When only a named set was read, every other row in the sidecar is still
	// current by construction and comes across untouched.
	if only != nil && prev != nil {
		read := make(map[string]struct{}, len(entities))
		for _, e := range entities {
			read[e.ID] = struct{}{}
		}
		for _, old := range prev.Index().Entries {
			if _, ok := read[old.ID]; ok {
				continue // covered by the loop above
			}
			if only[old.ID] {
				continue // named but unreadable: the entity is gone, drop its rows
			}
			carry = append(carry, old)
		}
	}

	result.Carried = len(carry)
	if prev != nil {
		live := make(map[[2]string]struct{}, len(carry)+len(pending))
		for _, e := range carry {
			live[[2]string{e.ID, e.Field}] = struct{}{}
		}
		for _, p := range pending {
			live[[2]string{p.entry.ID, p.entry.Field}] = struct{}{}
		}
		for _, e := range prev.Index().Entries {
			if _, ok := live[[2]string{e.ID, e.Field}]; !ok {
				result.Dropped++
			}
		}
	}

	if dryRun {
		result.Embedded = len(pending)
		if prev != nil {
			result.Dim = prev.Dim()
		}
		return result, nil
	}

	// Sort the work so the embedding command sees a stable order. A batch
	// embedder that is order-sensitive is then at least reproducibly so.
	sort.Slice(pending, func(i, j int) bool {
		if pending[i].entry.ID != pending[j].entry.ID {
			return pending[i].entry.ID < pending[j].entry.ID
		}
		return pending[i].entry.Field < pending[j].entry.Field
	})

	fresh := make([][]float64, len(pending))
	if len(pending) > 0 {
		inputs := make([]EmbedInput, len(pending))
		for i, w := range pending {
			inputs[i] = EmbedInput{
				Text:     w.text,
				Field:    w.entry.Field,
				EntityID: w.entry.ID,
				Type:     w.entry.Type,
			}
		}
		if emb.Batch {
			vecs, err := RunEmbedBatch(emb.Command, inputs)
			if err != nil {
				return result, fmt.Errorf("batch embed: %w", err)
			}
			if len(vecs) != len(inputs) {
				return result, fmt.Errorf("batch embed returned %d vectors, expected %d", len(vecs), len(inputs))
			}
			fresh = vecs
		} else {
			for i, in := range inputs {
				v, err := RunEmbedSingle(emb.Command, in)
				if err != nil {
					result.Warnings = append(result.Warnings,
						fmt.Sprintf("%s/%s: %v", in.EntityID, in.Field, err))
					result.Errors++
					continue
				}
				fresh[i] = v
			}
		}
	}

	dim := 0
	for _, v := range fresh {
		if len(v) > 0 {
			dim = len(v)
			break
		}
	}
	if dim == 0 && prev != nil {
		dim = prev.Dim()
	}
	if dim == 0 {
		// Nothing to embed and nothing carried: remove the sidecar so the
		// graph's derived state matches the graph.
		if err := vector.Remove(cacheDir); err != nil {
			return result, err
		}
		return result, nil
	}
	result.Dim = dim

	w := vector.NewWriter(cacheDir, emb.Digest(), dim)
	for _, e := range carry {
		row := prev.Row(e.Row)
		if len(row) != dim {
			// Dimension changed under us: the carried vector is not
			// comparable with the fresh ones, so re-embedding is the only
			// honest answer. Report it and drop the row.
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("%s/%s: stored vector has %d dimensions, embedder now returns %d; run `kg sync --rebuild`", e.ID, e.Field, len(row), dim))
			result.Errors++
			continue
		}
		if err := w.Put(e, row); err != nil {
			return result, err
		}
	}
	for i, p := range pending {
		v := fresh[i]
		if len(v) == 0 {
			continue // the embedder failed for this one; already warned
		}
		if len(v) != dim {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("%s/%s: embedder returned %d dimensions, expected %d", p.entry.ID, p.entry.Field, len(v), dim))
			result.Errors++
			continue
		}
		if err := w.Put(p.entry, v); err != nil {
			return result, err
		}
		result.Embedded++
	}

	if err := w.Commit(); err != nil {
		return result, fmt.Errorf("writing vector sidecar: %w", err)
	}
	result.Rows = w.Len()

	// Fewer rows than pairs means two entity files claimed one id, so one of
	// them is unreachable. Saying so here is cheap and is the only place the
	// embedding pass can see it (PRINCIPLES.md violation #3).
	if collapsed := result.Embedded + result.Carried - result.Rows; collapsed > 0 {
		result.Warnings = append(result.Warnings, fmt.Sprintf(
			"%d of %d (entity, field) pairs collapsed onto %d rows: that many entities share an id with another file, and only one of each is reachable",
			collapsed, result.Embedded+result.Carried, result.Rows))
	}
	return result, nil
}

// StripLegacyEmbeddings removes *_embedding attributes from every entity file
// in the graph, rewriting only the files that carry one.
//
// The vectors are discarded, not kept: use MigrateLegacyVectors to harvest
// them into the sidecar instead. Both write against the source of truth, so
// both are commands the operator runs deliberately (`kg sync
// --strip-embeddings` and `kg sync --migrate-vectors`), never something a read
// triggers.
func StripLegacyEmbeddings(kgDir string, dryRun bool) (changed int, freed int64, err error) {
	files, err := ListEntityFiles(kgDir)
	if err != nil {
		return 0, 0, fmt.Errorf("listing entity files: %w", err)
	}
	for _, f := range files {
		var found []string
		for k := range f.Entity.Attributes {
			if isEmbeddingAttr(k) {
				found = append(found, k)
			}
		}
		if len(found) == 0 {
			continue
		}
		before, statErr := os.Stat(f.Path)
		for _, k := range found {
			delete(f.Entity.Attributes, k)
		}
		changed++
		if dryRun {
			if statErr == nil {
				freed += before.Size()
			}
			continue
		}
		// Written back to the file it came from, not to the path its id
		// implies: on this corpus those differ for hundreds of files, and
		// writing by id skips one file and overwrites another.
		if err := RewriteEntityFile(f.Path, f.Entity); err != nil {
			return changed, freed, err
		}
		if statErr == nil {
			if after, err := os.Stat(f.Path); err == nil {
				freed += before.Size() - after.Size()
			}
		}
	}
	return changed, freed, nil
}

// MigrateResult reports what moving vectors out of the entity files did.
type MigrateResult struct {
	// Harvested is the number of vectors moved into the sidecar.
	Harvested int `json:"harvested"`
	// Files is the number of entity files rewritten without their vectors.
	Files int `json:"files"`
	Dim   int `json:"dim,omitempty"`
	// Bytes is how much smaller the entity files got.
	Bytes    int64    `json:"bytes"`
	Warnings []string `json:"warnings,omitempty"`
}

// MigrateLegacyVectors moves vectors from the entity files into the sidecar.
//
// This is the one-way migration off ADR-0022's arrangement, and the reason it
// harvests rather than deletes: the vectors are real, they cost a model run to
// produce, and a corpus split should not require re-embedding 27,000 entities
// to get its search back.
//
// The sidecar is written before any entity file is touched, so an interrupted
// migration leaves the vectors in both places rather than neither.
//
// The embedder that produced the harvested vectors is unknown — the files do
// not record it — so the sidecar records no embedder digest and will be
// accepted by whichever one is configured next. An operator who is changing
// embedders in the same move wants `kg sync --rebuild`, not this.
func MigrateLegacyVectors(kgDir string, dryRun bool) (*MigrateResult, error) {
	files, err := ListEntityFiles(kgDir)
	if err != nil {
		return nil, fmt.Errorf("listing entity files: %w", err)
	}

	type harvested struct {
		entry vector.Entry
		vec   []float64
	}
	var rows []harvested
	result := &MigrateResult{}

	for _, f := range files {
		e := f.Entity
		var attrs []string
		for k := range e.Attributes {
			if isEmbeddingAttr(k) {
				attrs = append(attrs, k)
			}
		}
		sort.Strings(attrs)
		for _, k := range attrs {
			vec, ok := ExtractVector(e.Attributes[k])
			if !ok || len(vec) == 0 {
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("%s: %s is not a vector, left in place", f.Path, k))
				continue
			}
			if result.Dim == 0 {
				result.Dim = len(vec)
			}
			if len(vec) != result.Dim {
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("%s: %s has %d dimensions among %d-dimensional vectors, left in place", f.Path, k, len(vec), result.Dim))
				continue
			}
			field := strings.TrimSuffix(k, "_embedding")
			// The checksum records which text the vector belongs to, so the
			// next `kg sync` can tell fresh from stale. A vector whose text
			// field is gone gets no checksum, which reads as always stale.
			checksum := ""
			if text, ok := e.Attributes[field]; ok {
				checksum = TextChecksum(AttrText(text))
			}
			rows = append(rows, harvested{
				entry: vector.Entry{ID: e.ID, Type: e.Type, Field: field, Checksum: checksum},
				vec:   vec,
			})
		}
	}

	result.Harvested = len(rows)
	if dryRun {
		changed, freed, err := StripLegacyEmbeddings(kgDir, true)
		if err != nil {
			return result, err
		}
		result.Files = changed
		result.Bytes = freed
		return result, nil
	}

	if len(rows) > 0 {
		w := vector.NewWriter(CacheDir(kgDir), "", result.Dim)
		for _, r := range rows {
			if err := w.Put(r.entry, r.vec); err != nil {
				return result, err
			}
		}
		if err := w.Commit(); err != nil {
			return result, fmt.Errorf("writing vector sidecar: %w", err)
		}
	}

	changed, freed, err := StripLegacyEmbeddings(kgDir, false)
	result.Files = changed
	result.Bytes = freed
	return result, err
}
