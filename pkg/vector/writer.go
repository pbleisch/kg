package vector

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"time"
)

// Writer builds a new sidecar pair and swaps it in atomically.
//
// Every write is a whole-file write. Rows are assigned by sorting the entries,
// so the same set of (entity, field, vector) triples always produces the same
// bytes, and there are no holes to reclaim, no free list to persist, and no
// way for the index and the floats to drift apart. The cost is rewriting the
// bin file when one entity changes; at the ceiling ADR-0026 states that is a
// fraction of a second, and it buys a format with one state instead of two.
type Writer struct {
	cacheDir string
	embedder string
	dim      int

	entries []Entry
	vecs    [][]float64
	seen    map[[2]string]int
}

// NewWriter starts a sidecar for vectors of the given dimension.
func NewWriter(cacheDir, embedder string, dim int) *Writer {
	return &Writer{
		cacheDir: cacheDir,
		embedder: embedder,
		dim:      dim,
		seen:     make(map[[2]string]int),
	}
}

// Dim is the dimension the writer was opened for.
func (w *Writer) Dim() int { return w.dim }

// Len is the number of vectors staged so far.
func (w *Writer) Len() int { return len(w.entries) }

// Put stages one vector. The entry's Row is assigned at Commit; whatever it
// holds now is ignored. Putting the same (id, field) twice replaces the first,
// which is what re-embedding an entity means.
//
// The vector may alias a source Store's mapping: Commit reads it before the
// caller is expected to close that Store.
func (w *Writer) Put(e Entry, vec []float64) error {
	if len(vec) != w.dim {
		return fmt.Errorf("entity %s field %s: vector has %d dimensions, sidecar has %d", e.ID, e.Field, len(vec), w.dim)
	}
	e.Field = normalizeField(e.Field)
	key := [2]string{e.ID, e.Field}
	if i, ok := w.seen[key]; ok {
		w.entries[i] = e
		w.vecs[i] = vec
		return nil
	}
	w.seen[key] = len(w.entries)
	w.entries = append(w.entries, e)
	w.vecs = append(w.vecs, vec)
	return nil
}

// Drop removes a staged (id, field) pair. Used when an entity or one of its
// embedded fields disappears from the graph.
func (w *Writer) Drop(id, field string) {
	key := [2]string{id, normalizeField(field)}
	i, ok := w.seen[key]
	if !ok {
		return
	}
	delete(w.seen, key)
	w.entries = append(w.entries[:i], w.entries[i+1:]...)
	w.vecs = append(w.vecs[:i], w.vecs[i+1:]...)
	for k, j := range w.seen {
		if j > i {
			w.seen[k] = j - 1
		}
	}
}

// Has reports whether a pair is staged.
func (w *Writer) Has(id, field string) bool {
	_, ok := w.seen[[2]string{id, normalizeField(field)}]
	return ok
}

// Commit writes both files. The bin file lands first: an index that names rows
// a bin file does not have is the one inconsistency Open cannot tell from
// corruption, so the file that grows is always the one already on disk.
func (w *Writer) Commit() error {
	sortEntries(w.entries)
	// sortEntries moved the entries; rebuild the parallel vector slice from
	// the map before it goes stale.
	vecs := make([][]float64, len(w.entries))
	for i, e := range w.entries {
		vecs[i] = w.vecs[w.seen[[2]string{e.ID, e.Field}]]
		w.entries[i].Row = i
	}
	w.vecs = vecs
	for i, e := range w.entries {
		w.seen[[2]string{e.ID, e.Field}] = i
	}

	if err := writeAtomic(BinPath(w.cacheDir), func(f *os.File) error {
		bw := bufio.NewWriterSize(f, 1<<20)
		var buf [8]byte
		for i, vec := range w.vecs {
			for _, v := range vec {
				nativeEndian.PutUint64(buf[:], math.Float64bits(v))
				if _, err := bw.Write(buf[:]); err != nil {
					return fmt.Errorf("writing row %d: %w", i, err)
				}
			}
		}
		return bw.Flush()
	}); err != nil {
		return err
	}

	idx := &Index{
		Version:   FormatVersion,
		Dim:       w.dim,
		Rows:      len(w.entries),
		ByteOrder: hostByteOrder(),
		Embedder:  w.embedder,
		WrittenAt: time.Now().UTC(),
		Entries:   w.entries,
	}
	return writeAtomic(IdxPath(w.cacheDir), func(f *os.File) error {
		enc := newIndexEncoder(f)
		return enc.Encode(idx)
	})
}

// Remove deletes both sidecar files. A graph with no vectors is a valid state,
// so a missing file is not an error.
func Remove(cacheDir string) error {
	for _, p := range []string{BinPath(cacheDir), IdxPath(cacheDir)} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing %s: %w", p, err)
		}
	}
	return nil
}
