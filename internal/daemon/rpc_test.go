package daemon

import (
	"net"
	"net/rpc"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/pbleisch/kg/pkg/store"
)

// startTestRPCServer builds a KG index from seed entities, registers an
// IndexService on a fresh rpc.Server, listens on a Unix socket in tmpDir,
// and returns the socket path plus a cleanup function.
func startTestRPCServer(t *testing.T, kgDir string, entities []*store.Entity) (socketPath string, cleanup func()) {
	t.Helper()

	// Write seed entities to disk.
	for _, e := range entities {
		if err := store.WriteEntity(kgDir, e); err != nil {
			t.Fatalf("WriteEntity(%s): %v", e.ID, err)
		}
	}

	// Build the bolt index.
	if err := store.Sync(kgDir); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	idx, err := store.OpenIndex(kgDir)
	if err != nil {
		t.Fatalf("OpenIndex: %v", err)
	}

	svc := NewIndexService(kgDir, idx)

	srv := rpc.NewServer()
	if err := srv.RegisterName(ServiceName, svc); err != nil {
		idx.Close()
		t.Fatalf("RegisterName: %v", err)
	}

	socketPath = filepath.Join(kgDir, ".cache", "test-rpc.sock")
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		idx.Close()
		t.Fatalf("Listen: %v", err)
	}

	// Accept loop in the background.
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return // listener closed
			}
			go srv.ServeConn(conn)
		}
	}()

	cleanup = func() {
		ln.Close()
		idx.Close()
	}
	return socketPath, cleanup
}

// seedEntities returns a small set of entities useful for most subtests.
func seedEntities() []*store.Entity {
	return []*store.Entity{
		{
			ID:   "person:alice",
			Type: "person",
			Attributes: map[string]any{
				"name":            "Alice",
				"title":           "Engineer",
				"title_embedding": []float64{0.1, 0.2, 0.3, 0.4},
			},
			Relations: map[string][]string{
				"works_at": {"company:acme"},
			},
		},
		{
			ID:   "person:bob",
			Type: "person",
			Attributes: map[string]any{
				"name":            "Bob",
				"title":           "Designer",
				"title_embedding": []float64{0.9, 0.8, 0.7, 0.6},
			},
			Relations: map[string][]string{
				"works_at":   {"company:acme"},
				"reports_to": {"person:alice"},
			},
		},
		{
			ID:   "company:acme",
			Type: "company",
			Attributes: map[string]any{
				"name":     "Acme Corp",
				"industry": "Tech",
			},
		},
	}
}

func TestRPCRoundTrip(t *testing.T) {
	kgDir := setupTestKG(t)
	socketPath, cleanup := startTestRPCServer(t, kgDir, seedEntities())
	defer cleanup()

	client, err := store.DialRemote(socketPath)
	if err != nil {
		t.Fatalf("DialRemote: %v", err)
	}
	defer client.Close()

	t.Run("GetEntity", func(t *testing.T) {
		e, err := client.GetEntity("person:alice")
		if err != nil {
			t.Fatalf("GetEntity: %v", err)
		}
		if e.ID != "person:alice" {
			t.Errorf("ID = %q, want person:alice", e.ID)
		}
		if e.Type != "person" {
			t.Errorf("Type = %q, want person", e.Type)
		}
		if e.Attributes["name"] != "Alice" {
			t.Errorf("name = %v, want Alice", e.Attributes["name"])
		}
		if e.Attributes["title"] != "Engineer" {
			t.Errorf("title = %v, want Engineer", e.Attributes["title"])
		}
		rels := e.Relations["works_at"]
		if len(rels) != 1 || rels[0] != "company:acme" {
			t.Errorf("works_at = %v, want [company:acme]", rels)
		}
	})

	t.Run("GetEntity_NotFound", func(t *testing.T) {
		_, err := client.GetEntity("person:nobody")
		if err == nil {
			t.Fatal("expected error for missing entity, got nil")
		}
	})

	t.Run("FindByType", func(t *testing.T) {
		ids, err := client.FindByType("person")
		if err != nil {
			t.Fatalf("FindByType: %v", err)
		}
		sort.Strings(ids)
		if len(ids) != 2 {
			t.Fatalf("len = %d, want 2: %v", len(ids), ids)
		}
		if ids[0] != "person:alice" || ids[1] != "person:bob" {
			t.Errorf("ids = %v, want [person:alice person:bob]", ids)
		}
	})

	t.Run("FindByType_NoResults", func(t *testing.T) {
		ids, err := client.FindByType("nonexistent")
		if err != nil {
			t.Fatalf("FindByType: %v", err)
		}
		if len(ids) != 0 {
			t.Errorf("expected empty, got %v", ids)
		}
	})

	t.Run("FindByRelationTarget", func(t *testing.T) {
		ids, err := client.FindByRelationTarget("works_at", "company:acme")
		if err != nil {
			t.Fatalf("FindByRelationTarget: %v", err)
		}
		sort.Strings(ids)
		if len(ids) != 2 {
			t.Fatalf("len = %d, want 2: %v", len(ids), ids)
		}
		if ids[0] != "person:alice" || ids[1] != "person:bob" {
			t.Errorf("ids = %v, want [person:alice person:bob]", ids)
		}
	})

	t.Run("FindReferrers", func(t *testing.T) {
		ids, err := client.FindReferrers("company:acme")
		if err != nil {
			t.Fatalf("FindReferrers: %v", err)
		}
		sort.Strings(ids)
		if len(ids) != 2 {
			t.Fatalf("len = %d, want 2: %v", len(ids), ids)
		}
		if ids[0] != "person:alice" || ids[1] != "person:bob" {
			t.Errorf("ids = %v, want [person:alice person:bob]", ids)
		}

		// person:alice is referred to by person:bob (reports_to).
		ids2, err := client.FindReferrers("person:alice")
		if err != nil {
			t.Fatalf("FindReferrers(alice): %v", err)
		}
		if len(ids2) != 1 || ids2[0] != "person:bob" {
			t.Errorf("referrers of alice = %v, want [person:bob]", ids2)
		}
	})

	t.Run("AddEntity_GetEntity", func(t *testing.T) {
		newE := &store.Entity{
			ID:   "project:kg",
			Type: "project",
			Attributes: map[string]any{
				"name":   "Knowledge Graph",
				"status": "active",
			},
			Relations: map[string][]string{
				"owned_by": {"company:acme"},
			},
		}
		if err := client.AddEntity(newE); err != nil {
			t.Fatalf("AddEntity: %v", err)
		}

		got, err := client.GetEntity("project:kg")
		if err != nil {
			t.Fatalf("GetEntity: %v", err)
		}
		if got.ID != "project:kg" {
			t.Errorf("ID = %q, want project:kg", got.ID)
		}
		if got.Attributes["name"] != "Knowledge Graph" {
			t.Errorf("name = %v, want Knowledge Graph", got.Attributes["name"])
		}
		rels := got.Relations["owned_by"]
		if len(rels) != 1 || rels[0] != "company:acme" {
			t.Errorf("owned_by = %v, want [company:acme]", rels)
		}
	})

	t.Run("UpsertEntity", func(t *testing.T) {
		updated := &store.Entity{
			ID:   "company:acme",
			Type: "company",
			Attributes: map[string]any{
				"name":     "Acme Corp",
				"industry": "Software",
				"size":     "large",
			},
		}
		if err := client.UpsertEntity(updated); err != nil {
			t.Fatalf("UpsertEntity: %v", err)
		}

		got, err := client.GetEntity("company:acme")
		if err != nil {
			t.Fatalf("GetEntity: %v", err)
		}
		if got.Attributes["industry"] != "Software" {
			t.Errorf("industry = %v, want Software", got.Attributes["industry"])
		}
		if got.Attributes["size"] != "large" {
			t.Errorf("size = %v, want large", got.Attributes["size"])
		}
	})

	t.Run("RemoveEntity", func(t *testing.T) {
		// First add, then remove.
		tmp := &store.Entity{
			ID:         "tool:temp",
			Type:       "tool",
			Attributes: map[string]any{"name": "Temp"},
		}
		if err := client.AddEntity(tmp); err != nil {
			t.Fatalf("AddEntity: %v", err)
		}

		if err := client.RemoveEntity("tool:temp"); err != nil {
			t.Fatalf("RemoveEntity: %v", err)
		}

		_, err = client.GetEntity("tool:temp")
		if err == nil {
			t.Error("expected error after remove, got nil")
		}
	})

	t.Run("UpsertEntities_Batch", func(t *testing.T) {
		batch := []*store.Entity{
			{
				ID:         "tool:go",
				Type:       "tool",
				Attributes: map[string]any{"name": "Go"},
			},
			{
				ID:         "tool:rust",
				Type:       "tool",
				Attributes: map[string]any{"name": "Rust"},
			},
		}
		bi, ok := client.(store.BatchIndexer)
		if !ok {
			t.Fatal("client does not implement BatchIndexer")
		}
		if err := bi.UpsertEntities(batch); err != nil {
			t.Fatalf("UpsertEntities: %v", err)
		}

		for _, want := range batch {
			got, err := client.GetEntity(want.ID)
			if err != nil {
				t.Fatalf("GetEntity(%s): %v", want.ID, err)
			}
			if got.Attributes["name"] != want.Attributes["name"] {
				t.Errorf("%s name = %v, want %v", want.ID, got.Attributes["name"], want.Attributes["name"])
			}
		}
	})

	t.Run("RemoveEntities_Batch", func(t *testing.T) {
		bi, ok := client.(store.BatchIndexer)
		if !ok {
			t.Fatal("client does not implement BatchIndexer")
		}
		if err := bi.RemoveEntities([]string{"tool:go", "tool:rust"}); err != nil {
			t.Fatalf("RemoveEntities: %v", err)
		}

		for _, id := range []string{"tool:go", "tool:rust"} {
			_, err := client.GetEntity(id)
			if err == nil {
				t.Errorf("expected error for %s after batch remove, got nil", id)
			}
		}
	})

	t.Run("SimilarEntities", func(t *testing.T) {
		vs, ok := client.(store.VectorSearcher)
		if !ok {
			t.Fatal("client does not implement VectorSearcher")
		}

		// Query vector close to Alice's title_embedding [0.1, 0.2, 0.3, 0.4].
		results, err := vs.SimilarEntities(store.SimilarityParams{
			TypeName: "person",
			Vector:   []float64{0.1, 0.2, 0.3, 0.4},
			Fields:   []string{"title"},
			TopN:     2,
		})
		if err != nil {
			t.Fatalf("SimilarEntities: %v", err)
		}
		if len(results) == 0 {
			t.Fatal("expected at least one result")
		}
		// Alice should be the top match (exact vector).
		if results[0].Entity.ID != "person:alice" {
			t.Errorf("top result = %q, want person:alice", results[0].Entity.ID)
		}
		if results[0].Score < 0.99 {
			t.Errorf("top score = %f, want >= 0.99", results[0].Score)
		}
		if results[0].MatchField != "title_embedding" {
			t.Errorf("MatchField = %q, want title_embedding", results[0].MatchField)
		}
	})
}

func TestDaemonAvailable(t *testing.T) {
	t.Run("true_when_listening", func(t *testing.T) {
		// Use a short path to stay within Unix socket length limits.
		dir, err := os.MkdirTemp("", "kg")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(dir)
		socketPath := filepath.Join(dir, "t.sock")
		ln, err := net.Listen("unix", socketPath)
		if err != nil {
			t.Fatalf("Listen: %v", err)
		}
		defer ln.Close()

		if !store.DaemonAvailable(socketPath) {
			t.Error("DaemonAvailable = false, want true")
		}
	})

	t.Run("false_when_no_socket", func(t *testing.T) {
		dir := t.TempDir()
		bogus := filepath.Join(dir, "no-such.sock")
		if store.DaemonAvailable(bogus) {
			t.Error("DaemonAvailable = true, want false")
		}
	})

	t.Run("false_when_stale_file", func(t *testing.T) {
		dir := t.TempDir()
		stale := filepath.Join(dir, "stale.sock")
		if err := os.WriteFile(stale, []byte("not a socket"), 0o644); err != nil {
			t.Fatal(err)
		}
		if store.DaemonAvailable(stale) {
			t.Error("DaemonAvailable = true for stale file, want false")
		}
	})
}
