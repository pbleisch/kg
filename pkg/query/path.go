package query

import (
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

// Path traverses the graph from startID, following each step's relation in
// order, and returns the deduplicated set of entity IDs reached.
//
// Entities are read as the traversal reaches them, so a path costs the part of
// the graph it walks rather than the whole corpus.
func Path(kgDir string, startID string, steps []PathStep) ([]string, error) {
	l := store.NewLoader(kgDir)
	current := []string{startID}

	for _, step := range steps {
		if step.Recursive {
			current = walkRecursive(l, current, step.Relation)
		} else {
			current = walkOneHop(l, current, step.Relation)
		}
	}

	sort.Strings(current)
	return current, nil
}

// PathVerbose is like Path but returns intermediate results recording the
// depth and relation used to reach each entity.
func PathVerbose(kgDir string, startID string, steps []PathStep) ([]PathResult, error) {
	l := store.NewLoader(kgDir)
	current := []string{startID}
	var results []PathResult

	for i, step := range steps {
		depth := i + 1
		if step.Recursive {
			current, results = walkRecursiveVerbose(l, current, step.Relation, depth, results)
			continue
		}
		next := make(map[string]struct{})
		for _, id := range current {
			for _, t := range l.Relations(id, step.Relation) {
				if _, seen := next[t]; !seen {
					next[t] = struct{}{}
					results = append(results, PathResult{Depth: depth, Via: step.Relation, ID: t})
				}
			}
		}
		current = setToSlice(next)
	}

	return results, nil
}

// walkOneHop collects all relation targets for every ID in current,
// returning a deduplicated slice.
func walkOneHop(l *store.Loader, current []string, relation string) []string {
	seen := make(map[string]struct{})
	for _, id := range current {
		for _, t := range l.Relations(id, relation) {
			seen[t] = struct{}{}
		}
	}
	return setToSlice(seen)
}

// walkRecursive follows the relation transitively using BFS until no new IDs
// are found. Tracks visited IDs to handle cycles.
func walkRecursive(l *store.Loader, seeds []string, relation string) []string {
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
		for _, t := range l.Relations(id, relation) {
			if _, seen := visited[t]; !seen {
				visited[t] = struct{}{}
				collected = append(collected, t)
				queue = append(queue, t)
			}
		}
	}
	return collected
}

// walkRecursiveVerbose is like walkRecursive but appends PathResult entries.
func walkRecursiveVerbose(l *store.Loader, seeds []string, relation string, baseDepth int, results []PathResult) ([]string, []PathResult) {
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
		for _, t := range l.Relations(cur.id, relation) {
			if _, seen := visited[t]; !seen {
				visited[t] = struct{}{}
				collected = append(collected, t)
				results = append(results, PathResult{Depth: cur.depth, Via: relation, ID: t})
				queue = append(queue, item{id: t, depth: cur.depth + 1})
			}
		}
	}

	return collected, results
}

func setToSlice(m map[string]struct{}) []string {
	s := make([]string, 0, len(m))
	for k := range m {
		s = append(s, k)
	}
	sort.Strings(s)
	return s
}
