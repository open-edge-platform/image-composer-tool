package pkgsorter

import (
	"fmt"
	"sort"

	"slices"

	"github.com/open-edge-platform/image-composer-tool/internal/ospackage"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
)

// SortPackages takes a slice of packages and returns them in a valid installation order.
// This function is robust against dependency cycles. It identifies groups of cyclically
// dependent packages (Strongly Connected Components) and sorts them as single units.
func SortPackages(packages []ospackage.PackageInfo) ([]ospackage.PackageInfo, error) {
	logger := logger.Logger()
	logger.Infof("Sorting %d packages for installation using SCC-based topological sort", len(packages))
	if len(packages) == 0 {
		return []ospackage.PackageInfo{}, nil
	}

	// Build Adjacency List Graph
	// The graph maps a package to the list of packages it requires
	adj := make(map[string][]string)
	for _, pkg := range packages {
		// Ensure every package is a node in the graph, even if it has no dependencies.
		if _, ok := adj[pkg.Name]; !ok {
			adj[pkg.Name] = []string{}
		}
		adj[pkg.Name] = append(adj[pkg.Name], pkg.Requires...)
	}

	// Find Strongly Connected Components (SCCs) using Tarjan's Algorithm
	sccs := findSCCs(adj)

	// Build the Condensation Graph (a graph of the SCCs) ---
	// This new graph will be a Directed Acyclic Graph (DAG).
	sccGraph := make(map[int][]int)
	sccInDegree := make(map[int]int)
	nodeToSccID := make(map[string]int)

	for i, scc := range sccs {
		sccInDegree[i] = 0
		sccGraph[i] = []int{}
		for _, node := range scc {
			nodeToSccID[node] = i
		}
	}

	for _, scc := range sccs {
		for _, node := range scc {
			for _, neighbor := range adj[node] {
				// If a dependency points to a node in a different SCC,
				// create an edge between the SCCs in the condensation graph.
				if nodeToSccID[node] != nodeToSccID[neighbor] {
					fromSccID := nodeToSccID[neighbor]
					toSccID := nodeToSccID[node]

					// Add edge and update in-degree, avoiding duplicates.
					found := slices.Contains(sccGraph[fromSccID], toSccID)
					if !found {
						sccGraph[fromSccID] = append(sccGraph[fromSccID], toSccID)
						sccInDegree[toSccID]++
					}
				}
			}
		}
	}

	// Topologically Sort the Condensation Graph
	var queue []int
	for i := range sccs {
		if sccInDegree[i] == 0 {
			queue = append(queue, i)
		}
	}
	// Sort the initial queue for deterministic output when multiple nodes have no dependencies.
	sort.Ints(queue)

	var sortedSccIndices []int
	for len(queue) > 0 {
		sccID := queue[0]
		queue = queue[1:]
		sortedSccIndices = append(sortedSccIndices, sccID)

		// Sort the neighbors to ensure deterministic processing order.
		neighbors := sccGraph[sccID]
		sort.Ints(neighbors)

		for _, neighborSccID := range neighbors {
			sccInDegree[neighborSccID]--
			if sccInDegree[neighborSccID] == 0 {
				queue = append(queue, neighborSccID)
			}
		}
		// Keep the main queue sorted as well for full determinism.
		sort.Ints(queue)
	}

	// Flatten the sorted SCCs into the final package list
	packageMap := make(map[string]ospackage.PackageInfo)
	for _, p := range packages {
		packageMap[p.Name] = p
	}

	var sortedPackages []ospackage.PackageInfo
	for _, sccID := range sortedSccIndices {
		scc := sccs[sccID]
		// The order within a cycle doesn't matter, so we sort alphabetically
		// for a deterministic result.
		sort.Strings(scc)
		for _, pkgName := range scc {
			sortedPackages = append(sortedPackages, packageMap[pkgName])
		}
	}

	// This check ensures that if the condensation graph itself had a cycle
	// (which shouldn't happen) or if some packages were missed, we error out.
	if len(sortedPackages) != len(packages) {
		return nil, fmt.Errorf("failed to sort all packages, %d sorted out of %d. This may indicate a problem with the dependency graph construction", len(sortedPackages), len(packages))
	}

	// Log the final sorted order
	logger.Debugf("--- Final Installation Order ---")
	for i, pkg := range sortedPackages {
		logger.Debugf("[%d]: %s", i+1, pkg.Name)
	}
	logger.Debugf("--------------------------------")
	return sortedPackages, nil
}

// Tarjan's Algorithm for finding SCCs ---
type tarjan struct {
	adj       map[string][]string
	stack     []string
	onStack   map[string]bool
	visited   map[string]bool
	ids       map[string]int
	low       map[string]int
	sccs      [][]string
	idCounter int
}

func findSCCs(adj map[string][]string) [][]string {
	t := &tarjan{
		adj:     adj,
		onStack: make(map[string]bool),
		visited: make(map[string]bool),
		ids:     make(map[string]int),
		low:     make(map[string]int),
	}

	// Sort the nodes to ensure a deterministic traversal order for Tarjan's algorithm.
	var nodes []string
	for node := range adj {
		nodes = append(nodes, node)
	}
	sort.Strings(nodes)

	for _, node := range nodes {
		if !t.visited[node] {
			t.dfs(node)
		}
	}
	return t.sccs
}

func (t *tarjan) dfs(at string) {
	t.stack = append(t.stack, at)
	t.onStack[at] = true
	t.visited[at] = true
	t.ids[at] = t.idCounter
	t.low[at] = t.idCounter
	t.idCounter++

	// Sort neighbors to ensure deterministic traversal.
	neighbors := t.adj[at]
	sort.Strings(neighbors)

	for _, to := range neighbors {
		// If the dependency isn't in our graph, we can't traverse it.
		// This can happen if a 'requires' points to a package not in the list
		if _, ok := t.adj[to]; !ok {
			continue
		}
		if !t.visited[to] {
			t.dfs(to)
		}
		if t.onStack[to] {
			t.low[at] = min(t.low[at], t.low[to])
		}
	}

	if t.ids[at] == t.low[at] {
		var scc []string
		for {
			node := t.stack[len(t.stack)-1]
			t.stack = t.stack[:len(t.stack)-1]
			t.onStack[node] = false
			scc = append(scc, node)
			if node == at {
				break
			}
		}
		t.sccs = append(t.sccs, scc)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
