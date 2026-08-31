package store

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"

	"github.com/pbleisch/kg/pkg/vector"
)

// SyncResult reports what a rebuild of the derived data did.
type SyncResult struct {
	// Embedded, Carried and Dropped mirror EmbedResult: how many vectors were
	// computed, reused, and discarded.
	Embedded int `json:"embedded"`
	Carried  int `json:"carried"`
	Dropped  int `json:"dropped"`
	Errors   int `json:"errors"`
	// Rows is how many vectors the sidecar holds.
	Rows int `json:"rows"`
	Dim  int `json:"dim,omitempty"`
	// Entities is the number of entity files the graph holds.
	Entities int      `json:"entities"`
	Warnings []string `json:"warnings,omitempty"`
}

// SyncOptions controls a rebuild.
type SyncOptions struct {
	// Rebuild discards the existing sidecar and re-embeds every field rather
	// than carrying unchanged vectors over.
	Rebuild bool
	// NoEmbed skips embedding. A graph with no embedder configured has no
	// derived data to build, and this is how a caller says so deliberately
	// instead of finding out by getting no vectors.
	NoEmbed bool
	// DryRun reports what would be done without running the embedder or
	// writing anything.
	DryRun bool
	// Only restricts re-embedding to these entity ids.
	Only []string
}

// Sync rebuilds every derived artifact from the entity files.
//
// There is one derived artifact now — the vector sidecar under
// <kgDir>/.cache/ — so this is the one command P2 asks for: delete
// <kgDir>/.cache/, run `kg sync`, and `git status` is clean.
//
// Reads do not call this. Nothing is kept in sync with the graph any more,
// because nothing but the files answers a read; the sidecar exists only for
// `kg similar`, and it says which text it was built from so a caller can tell
// when it is behind.
func Sync(kgDir string, opts SyncOptions) (*SyncResult, error) {
	result := &SyncResult{}

	entities, err := ListEntities(kgDir)
	if err != nil {
		return nil, fmt.Errorf("listing entities: %w", err)
	}
	result.Entities = len(entities)

	if opts.NoEmbed {
		return result, nil
	}

	emb, err := ResolveEmbedder(kgDir)
	if err != nil {
		return result, err
	}

	if opts.Rebuild && !opts.DryRun {
		if err := vector.Remove(CacheDir(kgDir)); err != nil {
			return result, err
		}
	}

	er, err := EmbedWith(kgDir, emb, opts.Rebuild, opts.DryRun, opts.Only...)
	if er != nil {
		result.Embedded = er.Embedded
		result.Carried = er.Carried
		result.Dropped = er.Dropped
		result.Errors = er.Errors
		result.Rows = er.Rows
		result.Dim = er.Dim
		result.Warnings = append(result.Warnings, er.Warnings...)
	}
	if err != nil {
		return result, err
	}
	return result, nil
}

// FileChecksum computes the SHA-256 hex digest of a file.
func FileChecksum(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
