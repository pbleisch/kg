package store

import (
	"encoding/gob"
	"net"
	"net/rpc"
	"time"
)

func init() {
	// Register concrete types that appear inside map[string]any (Entity.Attributes)
	// so gob can encode/decode them across the RPC boundary.
	gob.Register(map[string]any{})
	gob.Register([]any{})
	gob.Register([]float64{})
	gob.Register([]string{})
	gob.Register([]map[string]any{})
}

// RPC service name — must match the constant in internal/daemon.
const rpcServiceName = "IndexService"

// ── Duplicated RPC wire types ───────────────────────────────────────────
//
// These mirror the structs in internal/daemon/rpc_types.go.
// We duplicate them here because pkg/store cannot import internal/daemon.
// gob encodes by struct field names, so identical field names and types
// in different packages encode/decode correctly.

type rpcEmpty struct{}

type rpcIDsReply struct{ IDs []string }

type rpcGetEntityArgs struct{ ID string }
type rpcGetEntityReply struct{ Entity *Entity }

type rpcFindByTypeArgs struct{ TypeName string }

type rpcFindEntitiesArgs struct {
	TypeName string
	Filters  []AttrFilter
}
type rpcFindEntitiesReply struct{ Entities []*Entity }

type rpcFindByRelTargetArgs struct {
	Relation string
	TargetID string
}

type rpcFindReferrersArgs struct{ ID string }

type rpcEntityArgs struct{ Entity *Entity }

type rpcRemoveEntityArgs struct{ ID string }

type rpcEntitiesArgs struct{ Entities []*Entity }

type rpcRemoveEntitiesArgs struct{ IDs []string }

type rpcSimilarArgs struct{ Params SimilarityParams }
type rpcSimilarReply struct{ Results []SimilarityResult }

// ── remoteIndex ─────────────────────────────────────────────────────────

// remoteIndex proxies Indexer, BatchIndexer, and VectorSearcher calls
// over net/rpc to a running daemon process.
type remoteIndex struct {
	client *rpc.Client
}

// DialRemote connects to the daemon RPC socket and returns an Indexer.
// The returned Indexer also implements BatchIndexer and VectorSearcher.
func DialRemote(socketPath string) (Indexer, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, err
	}
	return &remoteIndex{client: rpc.NewClient(conn)}, nil
}

// DaemonAvailable checks if a daemon RPC socket is accepting connections.
// It does a quick dial with a short timeout and closes immediately.
func DaemonAvailable(socketPath string) bool {
	conn, err := net.DialTimeout("unix", socketPath, 100*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// call is a helper that prefixes the service name.
func (r *remoteIndex) call(method string, args any, reply any) error {
	return r.client.Call(rpcServiceName+"."+method, args, reply)
}

// ── Indexer ─────────────────────────────────────────────────────────────

func (r *remoteIndex) Close() error {
	return r.client.Close()
}

func (r *remoteIndex) AddEntity(e *Entity) error {
	return r.call("AddEntity", &rpcEntityArgs{Entity: e}, &rpcEmpty{})
}

func (r *remoteIndex) RemoveEntity(id string) error {
	return r.call("RemoveEntity", &rpcRemoveEntityArgs{ID: id}, &rpcEmpty{})
}

func (r *remoteIndex) UpsertEntity(e *Entity) error {
	return r.call("UpsertEntity", &rpcEntityArgs{Entity: e}, &rpcEmpty{})
}

func (r *remoteIndex) GetEntity(id string) (*Entity, error) {
	var reply rpcGetEntityReply
	if err := r.call("GetEntity", &rpcGetEntityArgs{ID: id}, &reply); err != nil {
		return nil, err
	}
	return reply.Entity, nil
}

func (r *remoteIndex) FindByType(typeName string) ([]string, error) {
	var reply rpcIDsReply
	if err := r.call("FindByType", &rpcFindByTypeArgs{TypeName: typeName}, &reply); err != nil {
		return nil, err
	}
	return reply.IDs, nil
}

func (r *remoteIndex) FindEntities(typeName string, filters []AttrFilter) ([]*Entity, error) {
	var reply rpcFindEntitiesReply
	if err := r.call("FindEntities", &rpcFindEntitiesArgs{TypeName: typeName, Filters: filters}, &reply); err != nil {
		return nil, err
	}
	return reply.Entities, nil
}

func (r *remoteIndex) FindByRelationTarget(relation, targetID string) ([]string, error) {
	var reply rpcIDsReply
	err := r.call("FindByRelationTarget", &rpcFindByRelTargetArgs{
		Relation: relation,
		TargetID: targetID,
	}, &reply)
	if err != nil {
		return nil, err
	}
	return reply.IDs, nil
}

func (r *remoteIndex) FindReferrers(id string) ([]string, error) {
	var reply rpcIDsReply
	if err := r.call("FindReferrers", &rpcFindReferrersArgs{ID: id}, &reply); err != nil {
		return nil, err
	}
	return reply.IDs, nil
}

// ── BatchIndexer ────────────────────────────────────────────────────────

func (r *remoteIndex) UpsertEntities(entities []*Entity) error {
	return r.call("UpsertEntities", &rpcEntitiesArgs{Entities: entities}, &rpcEmpty{})
}

func (r *remoteIndex) RemoveEntities(ids []string) error {
	return r.call("RemoveEntities", &rpcRemoveEntitiesArgs{IDs: ids}, &rpcEmpty{})
}

// ── VectorSearcher ──────────────────────────────────────────────────────

func (r *remoteIndex) SimilarEntities(params SimilarityParams) ([]SimilarityResult, error) {
	var reply rpcSimilarReply
	if err := r.call("SimilarEntities", &rpcSimilarArgs{Params: params}, &reply); err != nil {
		return nil, err
	}
	return reply.Results, nil
}

// ── StatsProvider ───────────────────────────────────────────────────────

type rpcStatsReply struct{ Stats *IndexStats }

func (r *remoteIndex) IndexStats() (*IndexStats, error) {
	var reply rpcStatsReply
	if err := r.call("IndexStats", &rpcEmpty{}, &reply); err != nil {
		return nil, err
	}
	return reply.Stats, nil
}
