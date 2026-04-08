package daemon

import (
	"fmt"
	"sync"

	"github.com/pbleisch/kg/pkg/store"
)

// IndexService wraps a store.Indexer for net/rpc dispatch.
// Read methods acquire a read lock; write methods acquire a write lock.
type IndexService struct {
	mu    sync.RWMutex
	idx   store.Indexer
	kgDir string
}

// NewIndexService returns an IndexService backed by idx.
func NewIndexService(kgDir string, idx store.Indexer) *IndexService {
	return &IndexService{kgDir: kgDir, idx: idx}
}

// --- read methods (RLock) ---

func (s *IndexService) GetEntity(args *GetEntityArgs, reply *GetEntityReply) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, err := s.idx.GetEntity(args.ID)
	if err != nil {
		return err
	}
	reply.Entity = e
	return nil
}

func (s *IndexService) FindByType(args *FindByTypeArgs, reply *IDsReply) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids, err := s.idx.FindByType(args.TypeName)
	if err != nil {
		return err
	}
	reply.IDs = ids
	return nil
}

func (s *IndexService) FindByRelationTarget(args *FindByRelTargetArgs, reply *IDsReply) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids, err := s.idx.FindByRelationTarget(args.Relation, args.TargetID)
	if err != nil {
		return err
	}
	reply.IDs = ids
	return nil
}

func (s *IndexService) FindEntities(args *FindEntitiesArgs, reply *FindEntitiesReply) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entities, err := s.idx.FindEntities(args.TypeName, args.Opts)
	if err != nil {
		return err
	}
	reply.Entities = entities
	return nil
}

func (s *IndexService) FindReferrers(args *FindReferrersArgs, reply *IDsReply) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids, err := s.idx.FindReferrers(args.ID)
	if err != nil {
		return err
	}
	reply.IDs = ids
	return nil
}

func (s *IndexService) SimilarEntities(args *SimilarArgs, reply *SimilarReply) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	vs, ok := s.idx.(store.VectorSearcher)
	if !ok {
		return fmt.Errorf("index backend does not support vector search")
	}
	results, err := vs.SimilarEntities(args.Params)
	if err != nil {
		return err
	}
	reply.Results = results
	return nil
}

// --- write methods (Lock) ---

func (s *IndexService) AddEntity(args *EntityArgs, reply *Empty) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.idx.AddEntity(args.Entity)
}

func (s *IndexService) RemoveEntity(args *RemoveEntityArgs, reply *Empty) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.idx.RemoveEntity(args.ID)
}

func (s *IndexService) UpsertEntity(args *EntityArgs, reply *Empty) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.idx.UpsertEntity(args.Entity)
}

func (s *IndexService) UpsertEntities(args *EntitiesArgs, reply *Empty) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if bi, ok := s.idx.(store.BatchIndexer); ok {
		return bi.UpsertEntities(args.Entities)
	}
	for _, e := range args.Entities {
		if err := s.idx.UpsertEntity(e); err != nil {
			return err
		}
	}
	return nil
}

func (s *IndexService) RemoveEntities(args *RemoveEntitiesArgs, reply *Empty) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if bi, ok := s.idx.(store.BatchIndexer); ok {
		return bi.RemoveEntities(args.IDs)
	}
	for _, id := range args.IDs {
		if err := s.idx.RemoveEntity(id); err != nil {
			return err
		}
	}
	return nil
}

func (s *IndexService) Sync(_ *SyncArgs, reply *SyncReply) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := store.SyncWithIndex(s.kgDir, s.idx)
	if err != nil {
		return err
	}
	reply.Result = result
	return nil
}

func (s *IndexService) IndexStats(_ *Empty, reply *StatsReply) error {
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
	return nil
}
