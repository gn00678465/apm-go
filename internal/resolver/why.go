package resolver

import (
	"fmt"
	"sort"
	"strings"

	"github.com/apm-go/apm/internal/lockfile"
)

// WhyEdge represents one step in a dependency chain.
type WhyEdge struct {
	Key        string
	Constraint string
}

// WhyPath is a root-to-target chain.
type WhyPath struct {
	Edges []WhyEdge
}

func (p WhyPath) String() string {
	parts := make([]string, len(p.Edges))
	for i, e := range p.Edges {
		if e.Constraint != "" {
			parts[i] = e.Key + "@" + e.Constraint
		} else {
			parts[i] = e.Key
		}
	}
	return strings.Join(parts, " -> ")
}

// pathTuple returns a sortable representation for lexicographic ordering.
func (p WhyPath) pathTuple() string {
	parts := make([]string, len(p.Edges))
	for i, e := range p.Edges {
		parts[i] = e.Key
	}
	return strings.Join(parts, "\x00")
}

// ComputeWhy walks the lockfile bottom-up from targetKey to roots,
// returning all root-to-target chains sorted lexicographically (req-rs-005).
func ComputeWhy(lock *lockfile.Lockfile, targetKey string) ([]WhyPath, error) {
	if lock == nil {
		return nil, fmt.Errorf("no lockfile")
	}

	target := lock.FindByKey(targetKey)
	if target == nil {
		return nil, fmt.Errorf("package %q not found in lockfile", targetKey)
	}

	// Build reverse index: resolvedBy -> deps that point to it
	byKey := map[string]*lockfile.LockedDep{}
	for i := range lock.Dependencies {
		d := &lock.Dependencies[i]
		byKey[d.UniqueKey()] = d
	}

	// Walk bottom-up
	type workItem struct {
		dep     *lockfile.LockedDep
		chain   []WhyEdge
		visited map[string]bool
	}

	var paths []WhyPath
	maxPaths := 256

	initial := workItem{
		dep: target,
		chain: []WhyEdge{{
			Key:        target.UniqueKey(),
			Constraint: target.ResolvedRef,
		}},
		visited: map[string]bool{target.UniqueKey(): true},
	}

	worklist := []workItem{initial}

	for len(worklist) > 0 && len(paths) < maxPaths {
		item := worklist[len(worklist)-1]
		worklist = worklist[:len(worklist)-1]

		if item.dep.ResolvedBy == "" {
			// Reached a root (direct dep)
			paths = append(paths, WhyPath{Edges: reverseEdges(item.chain)})
			continue
		}

		parent := byKey[item.dep.ResolvedBy]
		if parent == nil {
			// Corrupt/partial lockfile — record what we have
			paths = append(paths, WhyPath{Edges: reverseEdges(item.chain)})
			continue
		}

		if item.visited[parent.UniqueKey()] {
			// Cycle detected — record and stop this path
			paths = append(paths, WhyPath{Edges: reverseEdges(item.chain)})
			continue
		}

		newVisited := make(map[string]bool, len(item.visited)+1)
		for k, v := range item.visited {
			newVisited[k] = v
		}
		newVisited[parent.UniqueKey()] = true

		newChain := make([]WhyEdge, len(item.chain)+1)
		copy(newChain, item.chain)
		newChain[len(item.chain)] = WhyEdge{
			Key:        parent.UniqueKey(),
			Constraint: parent.ResolvedRef,
		}

		worklist = append(worklist, workItem{
			dep:     parent,
			chain:   newChain,
			visited: newVisited,
		})
	}

	// Sort paths lexicographically by path tuple (req-rs-005)
	sort.Slice(paths, func(i, j int) bool {
		return paths[i].pathTuple() < paths[j].pathTuple()
	})

	return paths, nil
}

func reverseEdges(edges []WhyEdge) []WhyEdge {
	n := len(edges)
	reversed := make([]WhyEdge, n)
	for i := 0; i < n; i++ {
		reversed[i] = edges[n-1-i]
	}
	return reversed
}
