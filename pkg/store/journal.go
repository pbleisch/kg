package store

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// A Journal remembers the prior contents of every entity file mutated while it
// is open, so a batch that fails part-way can be put back.
//
// It sits in the store rather than beside the caller because the mutations it
// has to catch happen several frames down — a dedup merge rewrites referring
// entities the caller never named.  Recording at WriteEntity/DeleteEntity is
// the only place that sees all of them.
//
// One journal is active at a time, matching the existing process-global batch
// mode.  BeginJournal while another is open returns a handle that records
// nothing, so a nested caller cannot truncate the outer transaction.
type Journal struct {
	kgDir  string
	nested bool

	mu    sync.Mutex
	prior map[string][]byte // path -> contents before first mutation; nil = absent
}

var (
	journalMu     sync.RWMutex
	activeJournal *Journal
)

// BeginJournal starts recording mutations under kgDir.  Always pair it with
// Rollback or Close.
func BeginJournal(kgDir string) *Journal {
	journalMu.Lock()
	defer journalMu.Unlock()
	if activeJournal != nil {
		return &Journal{kgDir: kgDir, nested: true}
	}
	j := &Journal{kgDir: kgDir, prior: make(map[string][]byte)}
	activeJournal = j
	return j
}

// Close stops recording and keeps every change.
func (j *Journal) Close() {
	if j == nil || j.nested {
		return
	}
	journalMu.Lock()
	if activeJournal == j {
		activeJournal = nil
	}
	journalMu.Unlock()
}

// Touched reports how many distinct files the journal has recorded.
func (j *Journal) Touched() int {
	if j == nil || j.nested {
		return 0
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	return len(j.prior)
}

// Rollback restores every recorded file to its prior contents — removing the
// ones that did not exist — and stops recording.  It reports the first failure
// but attempts every path regardless, because a half-restored rollback is
// worse than a reported one.
func (j *Journal) Rollback() error {
	if j == nil || j.nested {
		return nil
	}
	j.mu.Lock()
	prior := j.prior
	j.prior = make(map[string][]byte)
	j.mu.Unlock()
	j.Close()

	var firstErr error
	keep := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	for path, contents := range prior {
		if contents == nil {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				keep(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			keep(err)
			continue
		}
		keep(os.WriteFile(path, contents, 0o644))
	}
	return firstErr
}

// journalNote records path's current contents, once, before it is mutated.
func journalNote(path string) {
	if path == "" {
		return
	}
	journalMu.RLock()
	j := activeJournal
	journalMu.RUnlock()
	if j == nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if _, seen := j.prior[path]; seen {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		j.prior[path] = nil // did not exist; rollback removes it
		return
	}
	j.prior[path] = data
}
