package query

import (
	"fmt"
	"sort"

	"github.com/pbleisch/kg/pkg/store"
)

// PathStep describes one hop in a relation traversal.
type PathStep struct {
	Relation  string
	Recursive bool
}

// PathResult records a single entity discovered during verbose traversal.
type PathResult struct {
	Depth int
	Via   string
	ID    string
}

// Path traverses the graph starting from startID, following each step's
// relation in order, and returns the deduplicated set of entity IDs reached.
func Path(kgDir string, startID string, steps []PathStep) ([]string, error) {
	if err := store.Sync(kgDir); err != nil {
		return nil, fmt.Errorf("sync: %w", err)
	}

	idx, err := store.OpenIndex(kgDir)
	if err != nil {
		return nil, fmt.Errorf("open index: %w", err)
	}
	defer idx.Close()

	current := []string{startID}

	for _, step := range steps {
		if step.Recursive {
			current, err = walkRecursive(idx, current, step.Relation)
		} else {
			current, err = walkOneHop(idx, current, step.Relation)
		}
		if err != nil {
			return nil, err
		}
	}

	sort.Strings(current)
	return current, nil
}

// PathVerbose is like Path but returns intermediate results recording the
// depth and relation used to reach each entity.
func PathVerbose(kgDir string, startID string, steps []PathStep) ([]PathResult, error) {
	if err := store.Sync(kgDir); err != nil {
		return nil, fmt.Errorf("sync: %w", err)
	}

	idx, err := store.OpenIndex(kgDir)
	if err != nil {
		return nil, fmt.Errorf("open index: %w", err)
	}
	defer idx.Close()

	current := []string{startID}
	var results []PathResult

	for i, step := range steps {
		depth := i + 1
		if step.Recursive {
			var reached []string
			reached, results, err = walkRecursiveVerbose(idx, current, step.Relation, depth, results)
			current = reached
		} else {
			next := make(map[string]struct{})
			for _, id := range current {
				targets, err := relTargets(idx, id, step.Relation)
				if err != nil {
					return nil, err
				}
				for _, t := range targets {
					if _, seen := next[t]; !seen {
						next[t] = struct{}{}
						results = append(results, PathResult{
							Depth: depth,
							Via:   step.Relation,
							ID:    t,
						})
					}
				}
			}
			current = setToSlice(next)
		}
		if err != nil {
			return nil, err
		}
	}

	return results, nil
}

// relTargets returns the relation targets for a single entity and relation name.
func relTargets(idx *store.Index, id, relation string) ([]string, error) {
	e, err := idx.GetEntity(id)
	if err != nil {
		return nil, nil // entity not found — no targets
	}
	return e.Relations[relation], nil
}

// walkOneHop collects all relation targets for every ID in current,
// returning a deduplicated slice.
func walkOneHop(idx *store.Index, current []string, relation string) ([]string, error) {
	seen := make(map[string]struct{})
	for _, id := range current {
		targets, err := relTargets(idx, id, relation)
		if err != nil {
			return nil, err
		}
		for _, t := range targets {
			seen[t] = struct{}{}
		}
	}
	return setToSlice(seen), nil
}

// walkRecursive follows the relation transitively using BFS until no new IDs
// are found. Tracks visited IDs to handle cycles.
func walkRecursive(idx *store.Index, seeds []string, relation string) ([]string, error) {
	visited := make(map[string]struct{})
	queue := make([]string, len(seeds))
	copy(queue, seeds)
	for _, s := range seeds {
		visited[s] = struct{}{}
	}

	var collected []string

	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]

		targets, err := relTargets(idx, id, relation)
		if err != nil {
			return nil, err
		}
		for _, t := range targets {
			if _, seen := visited[t]; !seen {
				visited[t] = struct{}{}
				collected = append(collected, t)
				queue = append(queue, t)
			}
		}
	}

	return collected, nil
}

// walkRecursiveVerbose is like walkRecursive but appends PathResult entries.
func walkRecursiveVerbose(idx *store.Index, seeds []string, relation string, baseDepth int, results []PathResult) ([]string, []PathResult, error) {
	type item struct {
		id    string
		depth int
	}

	visited := make(map[string]struct{})
	var queue []item
	for _, s := range seeds {
		visited[s] = struct{}{}
		queue = append(queue, item{id: s, depth: baseDepth})
	}

	var collected []string

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		targets, err := relTargets(idx, cur.id, relation)
		if err != nil {
			return nil, nil, err
		}
		for _, t := range targets {
			if _, seen := visited[t]; !seen {
				visited[t] = struct{}{}
				collected = append(collected, t)
				results = append(results, PathResult{
					Depth: cur.depth,
					Via:   relation,
					ID:    t,
				})
				queue = append(queue, item{id: t, depth: cur.depth + 1})
			}
		}
	}

	return collected, results, nil
}

func setToSlice(m map[string]struct{}) []string {
	s := make([]string, 0, len(m))
	for k := range m {
		s = append(s, k)
	}
	sort.Strings(s)
	return s
}
