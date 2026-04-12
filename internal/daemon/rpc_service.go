package daemon

import (
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"github.com/pbleisch/kg/pkg/store"
)

// IndexService wraps a store.Indexer for net/rpc dispatch.
// Read methods acquire a read lock; write methods acquire a write lock.
type IndexService struct {
	mu    sync.RWMutex
	idx   store.Indexer
	kgDir string
	log   *log.Logger
}

// NewIndexService returns an IndexService backed by idx.
func NewIndexService(kgDir string, idx store.Indexer) *IndexService {
	return &IndexService{kgDir: kgDir, idx: idx, log: log.New(io.Discard, "", 0)}
}

// --- read methods (RLock) ---

func (s *IndexService) GetEntity(args *GetEntityArgs, reply *GetEntityReply) error {
	start := time.Now()
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, err := s.idx.GetEntity(args.ID)
	if err != nil {
		s.log.Printf("rpc GetEntity(%s) error: %v (%s)", args.ID, err, time.Since(start).Round(time.Microsecond))
		return err
	}
	reply.Entity = e
	s.log.Printf("rpc GetEntity(%s) (%s)", args.ID, time.Since(start).Round(time.Microsecond))
	return nil
}

func (s *IndexService) FindByType(args *FindByTypeArgs, reply *IDsReply) error {
	start := time.Now()
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids, err := s.idx.FindByType(args.TypeName)
	if err != nil {
		s.log.Printf("rpc FindByType(%s) error: %v (%s)", args.TypeName, err, time.Since(start).Round(time.Microsecond))
		return err
	}
	reply.IDs = ids
	s.log.Printf("rpc FindByType(%s) → %d ids (%s)", args.TypeName, len(ids), time.Since(start).Round(time.Microsecond))
	return nil
}

func (s *IndexService) FindByRelationTarget(args *FindByRelTargetArgs, reply *IDsReply) error {
	start := time.Now()
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids, err := s.idx.FindByRelationTarget(args.Relation, args.TargetID)
	if err != nil {
		return err
	}
	reply.IDs = ids
	s.log.Printf("rpc FindByRelationTarget(%s, %s) → %d ids (%s)", args.Relation, args.TargetID, len(ids), time.Since(start).Round(time.Microsecond))
	return nil
}

func (s *IndexService) FindEntities(args *FindEntitiesArgs, reply *FindEntitiesReply) error {
	start := time.Now()
	s.mu.RLock()
	defer s.mu.RUnlock()
	entities, err := s.idx.FindEntities(args.TypeName, args.Opts)
	if err != nil {
		s.log.Printf("rpc FindEntities(%s) error: %v (%s)", args.TypeName, err, time.Since(start).Round(time.Microsecond))
		return err
	}
	reply.Entities = entities
	s.log.Printf("rpc FindEntities(%s) → %d entities (%s)", args.TypeName, len(entities), time.Since(start).Round(time.Microsecond))
	return nil
}

func (s *IndexService) FindReferrers(args *FindReferrersArgs, reply *IDsReply) error {
	start := time.Now()
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids, err := s.idx.FindReferrers(args.ID)
	if err != nil {
		return err
	}
	reply.IDs = ids
	s.log.Printf("rpc FindReferrers(%s) → %d ids (%s)", args.ID, len(ids), time.Since(start).Round(time.Microsecond))
	return nil
}

func (s *IndexService) SimilarEntities(args *SimilarArgs, reply *SimilarReply) error {
	start := time.Now()
	s.mu.RLock()
	defer s.mu.RUnlock()
	vs, ok := s.idx.(store.VectorSearcher)
	if !ok {
		return fmt.Errorf("index backend does not support vector search")
	}
	results, err := vs.SimilarEntities(args.Params)
	if err != nil {
		s.log.Printf("rpc SimilarEntities(%s, top=%d) error: %v (%s)", args.Params.TypeName, args.Params.TopN, err, time.Since(start).Round(time.Microsecond))
		return err
	}
	reply.Results = results
	s.log.Printf("rpc SimilarEntities(%s, top=%d) → %d results (%s)", args.Params.TypeName, args.Params.TopN, len(results), time.Since(start).Round(time.Microsecond))
	return nil
}

// --- write methods (Lock) ---

func (s *IndexService) AddEntity(args *EntityArgs, reply *Empty) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.idx.AddEntity(args.Entity)
	if err != nil {
		s.log.Printf("rpc AddEntity(%s) error: %v", args.Entity.ID, err)
	} else {
		s.log.Printf("rpc AddEntity(%s)", args.Entity.ID)
	}
	return err
}

func (s *IndexService) RemoveEntity(args *RemoveEntityArgs, reply *Empty) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.idx.RemoveEntity(args.ID)
	if err != nil {
		s.log.Printf("rpc RemoveEntity(%s) error: %v", args.ID, err)
	} else {
		s.log.Printf("rpc RemoveEntity(%s)", args.ID)
	}
	return err
}

func (s *IndexService) UpsertEntity(args *EntityArgs, reply *Empty) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.idx.UpsertEntity(args.Entity)
	if err != nil {
		s.log.Printf("rpc UpsertEntity(%s) error: %v", args.Entity.ID, err)
	} else {
		s.log.Printf("rpc UpsertEntity(%s)", args.Entity.ID)
	}
	return err
}

func (s *IndexService) UpsertEntities(args *EntitiesArgs, reply *Empty) error {
	start := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	var err error
	if bi, ok := s.idx.(store.BatchIndexer); ok {
		err = bi.UpsertEntities(args.Entities)
	} else {
		for _, e := range args.Entities {
			if err = s.idx.UpsertEntity(e); err != nil {
				break
			}
		}
	}
	if err != nil {
		s.log.Printf("rpc UpsertEntities(%d) error: %v (%s)", len(args.Entities), err, time.Since(start).Round(time.Microsecond))
	} else {
		s.log.Printf("rpc UpsertEntities(%d) (%s)", len(args.Entities), time.Since(start).Round(time.Microsecond))
	}
	return err
}

func (s *IndexService) RemoveEntities(args *RemoveEntitiesArgs, reply *Empty) error {
	start := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	var err error
	if bi, ok := s.idx.(store.BatchIndexer); ok {
		err = bi.RemoveEntities(args.IDs)
	} else {
		for _, id := range args.IDs {
			if err = s.idx.RemoveEntity(id); err != nil {
				break
			}
		}
	}
	if err != nil {
		s.log.Printf("rpc RemoveEntities(%d) error: %v (%s)", len(args.IDs), err, time.Since(start).Round(time.Microsecond))
	} else {
		s.log.Printf("rpc RemoveEntities(%d) (%s)", len(args.IDs), time.Since(start).Round(time.Microsecond))
	}
	return err
}

func (s *IndexService) Sync(_ *SyncArgs, reply *SyncReply) error {
	start := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := store.SyncWithIndex(s.kgDir, s.idx)
	if err != nil {
		s.log.Printf("rpc Sync error: %v (%s)", err, time.Since(start).Round(time.Microsecond))
		return err
	}
	reply.Result = result
	s.log.Printf("rpc Sync (%s)", time.Since(start).Round(time.Microsecond))
	return nil
}

func (s *IndexService) IndexStats(_ *Empty, reply *StatsReply) error {
	start := time.Now()
	s.mu.RLock()
	defer s.mu.RUnlock()
	sp, ok := s.idx.(store.StatsProvider)
	if !ok {
		return fmt.Errorf("index backend does not support stats")
	}
	stats, err := sp.IndexStats()
	if err != nil {
		return err
	}
	reply.Stats = stats
	s.log.Printf("rpc IndexStats (%s)", time.Since(start).Round(time.Microsecond))
	return nil
}
