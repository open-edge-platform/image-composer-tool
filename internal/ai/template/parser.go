// Package template provides template parsing and metadata extraction for RAG indexing.
package template

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	// maxEmbeddingPackages limits the number of packages included in searchable text
	// to avoid excessively long embeddings that may reduce search quality
	maxEmbeddingPackages = 20
)

// Metadata represents the optional metadata section in a template.
type Metadata struct {
	// Description is a human-readable description for semantic matching
	Description string `yaml:"description,omitempty"`

	// UseCases is a list of use case descriptions for this template
	UseCases []string `yaml:"use_cases,omitempty"`

	// Keywords is a list of terms that help match user queries
	Keywords []string `yaml:"keywords,omitempty"`

	// Capabilities are feature tags (security, monitoring, performance)
	Capabilities []string `yaml:"capabilities,omitempty"`

	// RecommendedFor contains natural language descriptions of ideal use cases
	RecommendedFor []string `yaml:"recommendedFor,omitempty"`
}

// TemplateInfo holds parsed template information for indexing.
type TemplateInfo struct {
	// FilePath is the absolute path to the template file
	FilePath string

	// FileName is the base name of the template file
	FileName string

	// ImageName is the name from image.name field
	ImageName string

	// ImageVersion is the version from image.version field
	ImageVersion string

	// Distribution is the dist from target.dist field
	Distribution string

	// Architecture is the arch from target.arch field
	Architecture string

	// OS is the os from target.os field
	OS string

	// ImageType is the imageType from target.imageType field
	ImageType string

	// Packages is the list of packages from systemConfig.packages
	Packages []string

	// Metadata is the optional metadata section
	Metadata Metadata

	// RawContent is the original file content (for cache hashing)
	RawContent []byte
}

// rawTemplate is used for initial YAML parsing.
type rawTemplate struct {
	Metadata Metadata `yaml:"metadata,omitempty"`
	Image    struct {
		Name    string `yaml:"name"`
		Version string `yaml:"version"`
	} `yaml:"image"`
	Target struct {
		OS        string `yaml:"os"`
		Dist      string `yaml:"dist"`
		Arch      string `yaml:"arch"`
		ImageType string `yaml:"imageType"`
	} `yaml:"target"`
	SystemConfig struct {
		Name        string   `yaml:"name"`
		Description string   `yaml:"description"`
		Packages    []string `yaml:"packages"`
	} `yaml:"systemConfig"`
}

// ParseTemplate parses a template file and extracts information for indexing.
func ParseTemplate(filePath string) (*TemplateInfo, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read template file: %w", err)
	}

	var raw rawTemplate
	if err := yaml.Unmarshal(content, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse template YAML: %w", err)
	}

	info := &TemplateInfo{
		FilePath:     filePath,
		FileName:     filepath.Base(filePath),
		ImageName:    raw.Image.Name,
		ImageVersion: raw.Image.Version,
		Distribution: raw.Target.Dist,
		Architecture: raw.Target.Arch,
		OS:           raw.Target.OS,
		ImageType:    raw.Target.ImageType,
		Packages:     raw.SystemConfig.Packages,
		Metadata:     raw.Metadata,
		RawContent:   content,
	}

	// If metadata description is empty, use systemConfig description
	if info.Metadata.Description == "" && raw.SystemConfig.Description != "" {
		info.Metadata.Description = raw.SystemConfig.Description
	}

	return info, nil
}

// ScanTemplates walks a directory tree for template files and parses them.
// Templates are grouped into per-distribution subdirectories, so the walk is
// recursive; a non-recursive scan would silently skip every template.
func ScanTemplates(dir string) ([]*TemplateInfo, error) {
	var templates []*TemplateInfo

	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() {
			return nil
		}

		if !strings.HasSuffix(entry.Name(), ".yml") && !strings.HasSuffix(entry.Name(), ".yaml") {
			return nil
		}

		info, parseErr := ParseTemplate(path)
		if parseErr != nil {
			// Log warning but continue with other templates
			fmt.Fprintf(os.Stderr, "Warning: failed to parse template %s: %v\n", path, parseErr)
			return nil
		}

		templates = append(templates, info)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to read templates directory: %w", err)
	}

	return templates, nil
}

// BuildSearchableText constructs human-readable text from template info for embedding.
// This text is optimized for semantic search quality.
func (t *TemplateInfo) BuildSearchableText() string {
	var parts []string

	// Template identification
	parts = append(parts, fmt.Sprintf("Template: %s", t.FileName))
	if t.ImageName != "" {
		parts = append(parts, fmt.Sprintf("Name: %s", t.ImageName))
	}

	// Metadata fields (if present)
	if t.Metadata.Description != "" {
		parts = append(parts, fmt.Sprintf("Description: %s", t.Metadata.Description))
	}

	// Use cases from metadata
	if len(t.Metadata.UseCases) > 0 {
		parts = append(parts, fmt.Sprintf("Use cases: %s", strings.Join(t.Metadata.UseCases, "; ")))
	}

	// Target information
	if t.Distribution != "" {
		parts = append(parts, fmt.Sprintf("Distribution: %s", t.Distribution))
	}
	if t.OS != "" {
		parts = append(parts, fmt.Sprintf("OS: %s", t.OS))
	}
	if t.Architecture != "" {
		parts = append(parts, fmt.Sprintf("Architecture: %s", t.Architecture))
	}
	if t.ImageType != "" {
		parts = append(parts, fmt.Sprintf("Image type: %s", t.ImageType))
	}

	// Keywords from metadata
	if len(t.Metadata.Keywords) > 0 {
		parts = append(parts, fmt.Sprintf("Keywords: %s", strings.Join(t.Metadata.Keywords, ", ")))
	}

	// Capabilities from metadata
	if len(t.Metadata.Capabilities) > 0 {
		parts = append(parts, fmt.Sprintf("Capabilities: %s", strings.Join(t.Metadata.Capabilities, ", ")))
	}

	// Recommended for from metadata
	if len(t.Metadata.RecommendedFor) > 0 {
		parts = append(parts, fmt.Sprintf("Recommended for: %s", strings.Join(t.Metadata.RecommendedFor, ", ")))
	}

	// Packages (limited to avoid too long text)
	if len(t.Packages) > 0 {
		pkgList := t.Packages
		if len(pkgList) > maxEmbeddingPackages {
			pkgList = pkgList[:maxEmbeddingPackages]
		}
		parts = append(parts, fmt.Sprintf("Packages: %s", strings.Join(pkgList, ", ")))
	}

	// Infer keywords from filename if no metadata keywords
	if len(t.Metadata.Keywords) == 0 {
		inferredKeywords := t.inferKeywordsFromFilename()
		if len(inferredKeywords) > 0 {
			parts = append(parts, fmt.Sprintf("Keywords: %s", strings.Join(inferredKeywords, ", ")))
		}
	}

	return strings.Join(parts, "\n")
}

// inferKeywordsFromFilename extracts keywords from the template filename.
func (t *TemplateInfo) inferKeywordsFromFilename() []string {
	var keywords []string
	name := strings.TrimSuffix(t.FileName, filepath.Ext(t.FileName))

	// Common keywords to look for
	keywordPatterns := []string{
		"cloud", "edge", "minimal", "raw", "iso", "initrd",
		"desktop", "server", "dlstreamer", "emf", "rt",
	}

	nameLower := strings.ToLower(name)
	for _, kw := range keywordPatterns {
		if strings.Contains(nameLower, kw) {
			keywords = append(keywords, kw)
		}
	}

	return keywords
}

// GetAllKeywords returns all keywords including inferred ones.
func (t *TemplateInfo) GetAllKeywords() []string {
	keywords := make([]string, len(t.Metadata.Keywords))
	copy(keywords, t.Metadata.Keywords)

	// Add inferred keywords if no explicit keywords
	if len(keywords) == 0 {
		keywords = append(keywords, t.inferKeywordsFromFilename()...)
	}

	// Add capabilities as keywords
	keywords = append(keywords, t.Metadata.Capabilities...)

	return keywords
}

// GetPackageSet returns a set of package names for quick lookup.
func (t *TemplateInfo) GetPackageSet() map[string]bool {
	set := make(map[string]bool)
	for _, pkg := range t.Packages {
		set[pkg] = true
		// Also add base name without version suffixes
		if idx := strings.Index(pkg, "="); idx > 0 {
			set[pkg[:idx]] = true
		}
	}
	return set
}
