package debutils

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/open-edge-platform/image-composer-tool/internal/ospackage"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
)

// MinimalPackageInfo contains only essential fields for reporting.
type MinimalPackageInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Origin  string `json:"origin"`
	URL     string `json:"url"`
	Parent  string `json:"parent,omitempty"`
	Child   string `json:"child,omitempty"`
	Found   bool   `json:"found"`
}

// DependencyChain represents a chain of dependencies for reporting.
type DependencyChain struct {
	Chain []MinimalPackageInfo `json:"trace"`
}

type MissingReport struct {
	ReportType string                       `json:"report_type"`
	Missing    map[string][]DependencyChain `json:"missing"`
}

func AddParentChildPair(parent ospackage.PackageInfo, child ospackage.PackageInfo, pairs *[][]ospackage.PackageInfo) {
	*pairs = append(*pairs, []ospackage.PackageInfo{parent, child})
}

// If child is missing, create an empty PackageInfo with just the name
func AddParentMissingChildPair(parent ospackage.PackageInfo, missingChildName string, pairs *[][]ospackage.PackageInfo) {
	child := ospackage.PackageInfo{Name: missingChildName}
	*pairs = append(*pairs, []ospackage.PackageInfo{parent, child})
}

// BuildDependencyChains constructs readable dependency chains from parentChildPairs,
// writes them as a JSON array to a file in /tmp, and returns the file path.
func BuildDependencyChains(parentChildPairs [][]ospackage.PackageInfo) string {
	// Build adjacency list with MinimalPackageInfo
	graph := make(map[string][]MinimalPackageInfo)
	parents := make(map[string]MinimalPackageInfo)
	children := make(map[string]MinimalPackageInfo)

	// Convert ospackage.PackageInfo to MinimalPackageInfo for all pairs
	toMinimal := func(pkg ospackage.PackageInfo) MinimalPackageInfo {
		return MinimalPackageInfo{
			Name:    pkg.Name,
			Version: pkg.Version,
			Origin:  pkg.Origin,
			URL:     pkg.URL,
		}
	}

	for _, pair := range parentChildPairs {
		if len(pair) != 2 {
			continue
		}
		parent := toMinimal(pair[0])
		child := toMinimal(pair[1])

		// Handle missing child
		if strings.Contains(child.Name, "(missing)") {
			child.Found = false
			child.Name = strings.ReplaceAll(child.Name, "(missing)", "")
		} else {
			child.Found = true
		}

		// Handle missing parent (rare, but for completeness)
		if strings.Contains(parent.Name, "(missing)") {
			parent.Found = false
			parent.Name = strings.ReplaceAll(parent.Name, "(missing)", "")
		} else {
			parent.Found = true
		}

		if parent.Name == "" || child.Name == "" {
			continue
		}
		parent.Child = child.Name
		child.Parent = parent.Name
		graph[parent.Name] = append(graph[parent.Name], child)
		parents[parent.Name] = parent
		children[child.Name] = child
	}

	// Find root nodes (parents that are not children)
	var roots []MinimalPackageInfo
	for _, p := range parents {
		if _, ok := children[p.Name]; !ok {
			roots = append(roots, p)
		}
	}

	// DFS to build chains
	report := MissingReport{
		ReportType: "missing_dependencies_report",
		Missing:    make(map[string][]DependencyChain),
	}

	var dfs func(node MinimalPackageInfo, path []MinimalPackageInfo)
	dfs = func(node MinimalPackageInfo, path []MinimalPackageInfo) {
		path = append(path, node)
		if next, ok := graph[node.Name]; ok && len(next) > 0 {
			for _, child := range next {
				dfs(child, path)
			}
		} else {
			// Only report if the last node is a missing package (contains "(missing)")
			missingName := path[len(path)-1].Name
			if !path[len(path)-1].Found {
				report.Missing[missingName] = append(report.Missing[missingName], DependencyChain{Chain: path})
			}
		}
	}

	for _, root := range roots {
		dfs(root, []MinimalPackageInfo{})
	}

	// Write report to JSON file in builds
	if err := os.MkdirAll(ReportPath, 0755); err != nil {
		logger.Logger().Debugf("creating base path: %w", err)
		return ""
	}
	reportFullPath := filepath.Join(ReportPath, fmt.Sprintf("dependency_missing_report_%d.json", time.Now().UnixNano()))
	f, err := os.Create(reportFullPath)
	if err != nil {
		return ""
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		// Remove the incomplete/corrupt file
		f.Close()
		os.Remove(reportFullPath)
		logger.Logger().Debugf("fail creating report: %w", reportFullPath)
		return ""
	}

	return reportFullPath
}
