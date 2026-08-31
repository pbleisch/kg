package vector

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
	"unsafe"
)

// FormatVersion is the on-disk version of the sidecar pair. A sidecar written
// by a different version is discarded and rebuilt rather than migrated:
// it is derived data, so rebuilding is always correct and always cheaper than
// a migration path nobody tests.
const FormatVersion = 1

// File names under <kgDir>/.cache/.
const (
	BinName = "vectors.bin"
	IdxName = "vectors.idx"
)

// Entry records where one embedded (entity, field) pair lives in vectors.bin
// and what text produced it.
type Entry struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Field    string `json:"field"`
	Row      int    `json:"row"`
	Checksum string `json:"checksum"`
}

// Index is the vectors.idx payload: everything about the sidecar except the
// floats themselves.
type Index struct {
	Version int `json:"version"`
	Dim     int `json:"dim"`
	Rows    int `json:"rows"`
	// ByteOrder is "little" or "big" — the order the floats in vectors.bin
	// were written in. A sidecar whose order differs from the reading host's
	// is stale, not corrupt.
	ByteOrder string `json:"byte_order"`
	// Embedder is a SHA-256 digest of the embedding command that produced
	// these vectors. It changes when the operator changes embedders, which
	// is what makes a model swap invalidate the sidecar even though every
	// text checksum still matches. The command itself is not recorded: it
	// may carry credentials.
	Embedder  string    `json:"embedder"`
	WrittenAt time.Time `json:"written_at"`
	Entries   []Entry   `json:"entries"`
}

// EmbedderDigest hashes an embedding command for the Embedder field.
func EmbedderDigest(command string) string {
	h := sha256.Sum256([]byte(command))
	return fmt.Sprintf("sha256:%x", h)
}

// hostByteOrder reports the byte order of the running host.
func hostByteOrder() string {
	var probe uint16 = 1
	if *(*byte)(unsafe.Pointer(&probe)) == 1 {
		return "little"
	}
	return "big"
}

// nativeEndian is the byte order used to encode and decode vectors.bin on
// the fallback (non-mmap) path.
var nativeEndian binary.ByteOrder = func() binary.ByteOrder {
	if hostByteOrder() == "little" {
		return binary.LittleEndian
	}
	return binary.BigEndian
}()

// BinPath and IdxPath name the two sidecar files for a cache directory.
func BinPath(cacheDir string) string { return filepath.Join(cacheDir, BinName) }
func IdxPath(cacheDir string) string { return filepath.Join(cacheDir, IdxName) }

// sortEntries orders entries by (id, field) so a full rebuild produces the
// same row assignment every time.
func sortEntries(es []Entry) {
	sort.Slice(es, func(i, j int) bool {
		if es[i].ID != es[j].ID {
			return es[i].ID < es[j].ID
		}
		return es[i].Field < es[j].Field
	})
}

// LoadIndex reads vectors.idx. A missing file is not an error: it yields an
// empty index, which is the correct state for a graph that has never been
// embedded.
func LoadIndex(cacheDir string) (*Index, error) {
	data, err := os.ReadFile(IdxPath(cacheDir))
	if err != nil {
		if os.IsNotExist(err) {
			return &Index{Version: FormatVersion, ByteOrder: hostByteOrder()}, nil
		}
		return nil, fmt.Errorf("reading vector index: %w", err)
	}
	var idx Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("parsing vector index: %w", err)
	}
	return &idx, nil
}

// Usable reports whether an index can be read on this host with this
// embedder. An index that is not usable is stale, and the caller's remedy is
// always to rebuild it — never to repair it.
func (idx *Index) Usable(embedder string) bool {
	if idx == nil || idx.Version != FormatVersion || idx.Dim <= 0 {
		return false
	}
	if idx.ByteOrder != hostByteOrder() {
		return false
	}
	if embedder != "" && idx.Embedder != "" && idx.Embedder != embedder {
		return false
	}
	return true
}

// Lookup returns the entry for one (entity, field) pair.
func (idx *Index) Lookup(id, field string) (Entry, bool) {
	for _, e := range idx.Entries {
		if e.ID == id && e.Field == field {
			return e, true
		}
	}
	return Entry{}, false
}

// byKey builds a lookup map for callers that probe many pairs.
func (idx *Index) byKey() map[[2]string]Entry {
	m := make(map[[2]string]Entry, len(idx.Entries))
	for _, e := range idx.Entries {
		m[[2]string{e.ID, e.Field}] = e
	}
	return m
}

// writeAtomic writes data to path via a temp file, fsync and rename, so a
// crash mid-write leaves the previous sidecar rather than a truncated one.
func writeAtomic(path string, write func(*os.File) error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("creating %s: %w", tmp, err)
	}
	if err := write(f); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("syncing %s: %w", tmp, err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("closing %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("renaming %s: %w", tmp, err)
	}
	return nil
}

// newIndexEncoder returns a JSON encoder configured the way the sidecar index
// is always written: indented, so a human debugging a stale sidecar can read
// it, and with HTML escaping off, so entity ids keep their own bytes.
func newIndexEncoder(w *os.File) *json.Encoder {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", " ")
	return enc
}
