// Package daemon — RPC wire types for proxying Indexer, BatchIndexer, and
// VectorSearcher over a Unix socket using net/rpc.
package daemon

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pbleisch/kg/pkg/store"
)

// ServiceName is the net/rpc service name used when registering the index service.
const ServiceName = "IndexService"

// RPCSocketPath returns the Unix socket path for the daemon RPC service.
// If the natural path is too long for a Unix socket (≥100 bytes), a
// shorter path under os.TempDir() is used instead.
func RPCSocketPath(kgDir string) string {
	p := filepath.Join(kgDir, ".cache", "daemon-rpc.sock")
	if len(p) < 100 {
		return p
	}
	h := sha256.Sum256([]byte(kgDir))
	return filepath.Join(os.TempDir(), fmt.Sprintf("kg-rpc-%x.sock", h[:8]))
}

// ── Shared reply types ──────────────────────────────────────────────────

// Empty is the reply type for methods that return only an error.
type Empty struct{}

// IDsReply is the reply type for methods that return a slice of entity IDs.
type IDsReply struct {
	IDs []string
}

// ── GetEntity ───────────────────────────────────────────────────────────

type GetEntityArgs struct {
	ID string
}

type GetEntityReply struct {
	Entity *store.Entity
}

// ── FindByType ──────────────────────────────────────────────────────────

type FindByTypeArgs struct {
	TypeName string
}

// ── FindByRelationTarget ────────────────────────────────────────────────

type FindByRelTargetArgs struct {
	Relation string
	TargetID string
}

// ── FindReferrers ───────────────────────────────────────────────────────

type FindReferrersArgs struct {
	ID string
}

// ── AddEntity / UpsertEntity ────────────────────────────────────────────

type EntityArgs struct {
	Entity *store.Entity
}

// ── RemoveEntity ────────────────────────────────────────────────────────

type RemoveEntityArgs struct {
	ID string
}

// ── UpsertEntities (batch) ──────────────────────────────────────────────

type EntitiesArgs struct {
	Entities []*store.Entity
}

// ── RemoveEntities (batch) ──────────────────────────────────────────────

type RemoveEntitiesArgs struct {
	IDs []string
}

// ── SimilarEntities ─────────────────────────────────────────────────────

type SimilarArgs struct {
	Params store.SimilarityParams
}

type SimilarReply struct {
	Results []store.SimilarityResult
}

// ── Sync ────────────────────────────────────────────────────────────────

type SyncArgs struct {
	KGDir string
}

type SyncReply struct {
	Result *store.SyncResult
}

// ── IndexStats ──────────────────────────────────────────────────────────

type StatsReply struct {
	Stats *store.IndexStats
}
