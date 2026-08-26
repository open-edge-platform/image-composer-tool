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

type reportNode struct {
	Key  string
	Info MinimalPackageInfo
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
	graph := make(map[string][]reportNode)
	parents := make(map[string]reportNode)
	children := make(map[string]reportNode)

	// Convert ospackage.PackageInfo to MinimalPackageInfo for all pairs
	toReportNode := func(pkg ospackage.PackageInfo) reportNode {
		rawName := pkg.Name
		minimal := MinimalPackageInfo{
			Name:    strings.ReplaceAll(rawName, "(missing)", ""),
			Version: pkg.Version,
			Origin:  pkg.Origin,
			URL:     pkg.URL,
			Found:   !strings.Contains(rawName, "(missing)"),
		}

		return reportNode{
			Key:  strings.Join([]string{rawName, pkg.Version, pkg.Origin, pkg.URL}, "|"),
			Info: minimal,
		}
	}

	for _, pair := range parentChildPairs {
		if len(pair) != 2 {
			continue
		}
		parent := toReportNode(pair[0])
		child := toReportNode(pair[1])

		if parent.Info.Name == "" || child.Info.Name == "" {
			continue
		}

		parent.Info.Child = child.Info.Name
		child.Info.Parent = parent.Info.Name
		graph[parent.Key] = append(graph[parent.Key], child)
		parents[parent.Key] = parent
		children[child.Key] = child
	}

	// Find root nodes (parents that are not children)
	var roots []reportNode
	for key, parent := range parents {
		if _, ok := children[key]; !ok {
			roots = append(roots, parent)
		}
	}
	if len(roots) == 0 {
		for _, parent := range parents {
			roots = append(roots, parent)
		}
	}

	// DFS to build chains
	report := MissingReport{
		ReportType: "missing_dependencies_report",
		Missing:    make(map[string][]DependencyChain),
	}

	seenChains := make(map[string]struct{})

	var dfs func(node reportNode, path []MinimalPackageInfo, visiting map[string]struct{})
	dfs = func(node reportNode, path []MinimalPackageInfo, visiting map[string]struct{}) {
		path = append(path, node.Info)
		if next, ok := graph[node.Key]; ok && len(next) > 0 {
			for _, child := range next {
				if _, seen := visiting[child.Key]; seen {
					continue
				}

				visiting[child.Key] = struct{}{}
				dfs(child, path, visiting)
				delete(visiting, child.Key)
			}
		} else {
			// Only report if the last node is a missing package.
			missingName := path[len(path)-1].Name
			if !path[len(path)-1].Found {
				chainKeyParts := make([]string, 0, len(path))
				for _, item := range path {
					chainKeyParts = append(chainKeyParts, strings.Join([]string{item.Name, item.Version, item.URL}, "|"))
				}
				chainKey := strings.Join(chainKeyParts, "->")
				if _, exists := seenChains[chainKey]; exists {
					return
				}

				seenChains[chainKey] = struct{}{}
				report.Missing[missingName] = append(report.Missing[missingName], DependencyChain{Chain: path})
			}
		}
	}

	for _, root := range roots {
		visiting := map[string]struct{}{root.Key: {}}
		dfs(root, []MinimalPackageInfo{}, visiting)
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
