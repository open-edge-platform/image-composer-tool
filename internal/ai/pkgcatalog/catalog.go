package pkgcatalog

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/open-edge-platform/image-composer-tool/internal/ai/index"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
)

var log = logger.Logger()

//go:embed data/bundles.yaml
var embeddedBundlesYAML []byte

//go:embed data/packages.yaml
var embeddedPackagesYAML []byte

// Bundle is a curated, cross-repo capability set (Tier A candidate as defined in adr-nl-package-selection.md).
type Bundle struct {
	BundleID    string   `yaml:"id" json:"id"`
	Name        string   `yaml:"name" json:"name"`
	Description string   `yaml:"description" json:"description"`
	KeywordList []string `yaml:"keywords" json:"keywords"`
	Packages    []string `yaml:"packages" json:"packages"` // member package names
	Repos       []string `yaml:"repos" json:"repos"`       // repo codenames this bundle enables
}

// ID returns the unique bundle identifier (satisfies index.Item).
func (b *Bundle) ID() string { return b.BundleID }

// Keywords returns the bundle keywords (satisfies index.Item).
func (b *Bundle) Keywords() []string { return b.KeywordList }

// PackageNames returns the member package names (satisfies index.Item).
func (b *Bundle) PackageNames() []string { return b.Packages }

// SearchableText returns the concatenated text representation for embedding (satisfies index.Item).
func (b *Bundle) SearchableText() string {
	parts := []string{
		fmt.Sprintf("Bundle: %s", b.Name),
		fmt.Sprintf("ID: %s", b.BundleID),
	}
	if b.Description != "" {
		parts = append(parts, fmt.Sprintf("Description: %s", b.Description))
	}
	if len(b.KeywordList) > 0 {
		parts = append(parts, fmt.Sprintf("Keywords: %s", strings.Join(b.KeywordList, ", ")))
	}
	if len(b.Packages) > 0 {
		parts = append(parts, fmt.Sprintf("Packages: %s", strings.Join(b.Packages, ", ")))
	}
	if len(b.Repos) > 0 {
		parts = append(parts, fmt.Sprintf("Repositories: %s", strings.Join(b.Repos, ", ")))
	}
	return strings.Join(parts, "\n")
}

// Ensure Bundle satisfies index.Item at compile time.
var _ index.Item = (*Bundle)(nil)

// NewBundle creates and validates a new Bundle instance.
func NewBundle(id, name, description string, keywords, packages, repos []string) (*Bundle, error) {
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("pkgcatalog: bundle ID cannot be empty")
	}
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("pkgcatalog: bundle name cannot be empty for %s", id)
	}
	if len(packages) == 0 {
		return nil, fmt.Errorf("pkgcatalog: bundle %s must contain at least one package", id)
	}
	return &Bundle{
		BundleID:    id,
		Name:        name,
		Description: description,
		KeywordList: keywords,
		Packages:    packages,
		Repos:       repos,
	}, nil
}

// CuratedPackage is a curated or template-mined package entry (Tier B candidate as defined in adr-nl-package-selection.md).
type CuratedPackage struct {
	Name        string   `yaml:"name" json:"name"`
	Description string   `yaml:"description" json:"description"`
	Repo        string   `yaml:"repo" json:"repo"`
	CoOccurs    []string `yaml:"co_occurs" json:"co_occurs"` // co-occurrence set mined from templates
}

// ID returns the package name (satisfies index.Item).
func (p *CuratedPackage) ID() string { return p.Name }

// Keywords returns repo if present (satisfies index.Item).
func (p *CuratedPackage) Keywords() []string {
	if p.Repo != "" {
		return []string{p.Repo}
	}
	return nil
}

// PackageNames returns the package name slice (satisfies index.Item).
func (p *CuratedPackage) PackageNames() []string {
	return []string{p.Name}
}

// SearchableText returns the concatenated text representation for embedding (satisfies index.Item).
func (p *CuratedPackage) SearchableText() string {
	parts := []string{
		fmt.Sprintf("Package: %s", p.Name),
	}
	if p.Repo != "" {
		parts = append(parts, fmt.Sprintf("Repository: %s", p.Repo))
	}
	if p.Description != "" {
		parts = append(parts, fmt.Sprintf("Description: %s", p.Description))
	}
	if len(p.CoOccurs) > 0 {
		sortedCoOccurs := make([]string, len(p.CoOccurs))
		copy(sortedCoOccurs, p.CoOccurs)
		sort.Strings(sortedCoOccurs)
		parts = append(parts, fmt.Sprintf("CoOccurs: %s", strings.Join(sortedCoOccurs, ", ")))
	}
	return strings.Join(parts, "\n")
}

// Ensure CuratedPackage satisfies index.Item at compile time.
var _ index.Item = (*CuratedPackage)(nil)

// NewCuratedPackage creates and validates a new CuratedPackage instance.
func NewCuratedPackage(name, description, repo string, coOccurs []string) (*CuratedPackage, error) {
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("pkgcatalog: package name cannot be empty")
	}
	return &CuratedPackage{
		Name:        name,
		Description: description,
		Repo:        repo,
		CoOccurs:    coOccurs,
	}, nil
}

// Catalog holds all curated bundles and packages.
type Catalog struct {
	Bundles         []Bundle         `yaml:"bundles" json:"bundles"`
	CuratedPackages []CuratedPackage `yaml:"packages" json:"packages"`
}

type bundleWrapper struct {
	Bundles []Bundle `yaml:"bundles"`
}

type packageWrapper struct {
	Packages []CuratedPackage `yaml:"packages"`
}

// LoadCatalog loads the bundle and curated package catalog.
// If catalogDir is specified and contains bundles.yaml / packages.yaml, it loads them from disk.
// Otherwise, it falls back to the embedded YAML files.
func LoadCatalog(catalogDir string) (*Catalog, error) {
	var bundlesData []byte
	var packagesData []byte

	if catalogDir != "" {
		customBundlesPath := filepath.Join(catalogDir, "bundles.yaml")
		if data, err := os.ReadFile(customBundlesPath); err == nil {
			log.Infof("Loaded custom bundles catalog from %s", customBundlesPath)
			bundlesData = data
		}

		customPackagesPath := filepath.Join(catalogDir, "packages.yaml")
		if data, err := os.ReadFile(customPackagesPath); err == nil {
			log.Infof("Loaded custom packages catalog from %s", customPackagesPath)
			packagesData = data
		}
	}

	if len(bundlesData) == 0 {
		bundlesData = embeddedBundlesYAML
	}
	if len(packagesData) == 0 {
		packagesData = embeddedPackagesYAML
	}

	var bw bundleWrapper
	if err := yaml.Unmarshal(bundlesData, &bw); err != nil {
		return nil, fmt.Errorf("pkgcatalog: failed to parse bundles YAML: %w", err)
	}

	var pw packageWrapper
	if err := yaml.Unmarshal(packagesData, &pw); err != nil {
		return nil, fmt.Errorf("pkgcatalog: failed to parse packages YAML: %w", err)
	}

	catalog := &Catalog{
		Bundles:         bw.Bundles,
		CuratedPackages: pw.Packages,
	}

	return catalog, nil
}
