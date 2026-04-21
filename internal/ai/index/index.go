// Package index provides in-memory vector index with cosine similarity search.
package index

import (
	"math"
	"sort"
	"strings"
	"sync"

	"github.com/open-edge-platform/image-composer-tool/internal/ai/template"
)

// Document represents an indexed template with its embedding.
type Document struct {
	// TemplateInfo is the parsed template information
	TemplateInfo *template.TemplateInfo

	// Embedding is the vector representation
	Embedding []float32

	// SearchableText is the text used for generating the embedding
	SearchableText string

	// ContentHash is the hash of the template content
	ContentHash string
}

// SearchResult represents a search result with scoring details.
type SearchResult struct {
	// Document is the matched template document
	Document *Document

	// Score is the final combined score
	Score float64

	// SemanticScore is the cosine similarity score
	SemanticScore float64

	// KeywordScore is the keyword overlap score
	KeywordScore float64

	// PackageScore is the package matching score
	PackageScore float64
}

// Index is an in-memory vector index for semantic search.
type Index struct {
	documents []*Document
	mu        sync.RWMutex
}

// NewIndex creates a new empty index.
func NewIndex() *Index {
	return &Index{
		documents: make([]*Document, 0),
	}
}

// Add adds a document to the index.
func (idx *Index) Add(doc *Document) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.documents = append(idx.documents, doc)
}

// Clear removes all documents from the index.
func (idx *Index) Clear() {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.documents = make([]*Document, 0)
}

// Size returns the number of documents in the index.
func (idx *Index) Size() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return len(idx.documents)
}

// SearchOptions configures the hybrid search behavior.
type SearchOptions struct {
	// TopK is the maximum number of results to return
	TopK int

	// SemanticWeight is the weight for semantic similarity (default 0.70)
	SemanticWeight float64

	// KeywordWeight is the weight for keyword overlap (default 0.20)
	KeywordWeight float64

	// PackageWeight is the weight for package matching (default 0.10)
	PackageWeight float64

	// MinScore is the minimum score threshold
	MinScore float64

	// NegativeTerms are terms to penalize
	NegativeTerms []string

	// NegationPenalty is the penalty multiplier for negative terms (default 0.5)
	NegationPenalty float64
}

// DefaultSearchOptions returns default search options.
func DefaultSearchOptions() SearchOptions {
	return SearchOptions{
		TopK:            5,
		SemanticWeight:  0.70,
		KeywordWeight:   0.20,
		PackageWeight:   0.10,
		MinScore:        0.40,
		NegativeTerms:   nil,
		NegationPenalty: 0.5,
	}
}

// Search performs hybrid search with the given query embedding and tokens.
func (idx *Index) Search(queryEmbedding []float32, queryTokens []string, queryPackages []string, opts SearchOptions) []SearchResult {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	if len(idx.documents) == 0 {
		return nil
	}

	// Normalize query tokens to lowercase
	normalizedTokens := make([]string, len(queryTokens))
	for i, t := range queryTokens {
		normalizedTokens[i] = strings.ToLower(t)
	}

	normalizedPackages := make([]string, len(queryPackages))
	for i, p := range queryPackages {
		normalizedPackages[i] = strings.ToLower(p)
	}

	negativeLower := make([]string, len(opts.NegativeTerms))
	for i, t := range opts.NegativeTerms {
		negativeLower[i] = strings.ToLower(t)
	}

	results := make([]SearchResult, 0, len(idx.documents))

	for _, doc := range idx.documents {
		// Calculate semantic score
		semanticScore := cosineSimilarity(queryEmbedding, doc.Embedding)

		// Calculate keyword score
		keywordScore := calculateKeywordScore(normalizedTokens, doc.TemplateInfo)

		// Calculate package score
		packageScore := calculatePackageScore(normalizedPackages, doc.TemplateInfo)

		// Calculate combined score
		score := (opts.SemanticWeight * semanticScore) +
			(opts.KeywordWeight * keywordScore) +
			(opts.PackageWeight * packageScore)

		// Apply negation penalty
		if len(negativeLower) > 0 {
			penalty := calculateNegationPenalty(negativeLower, doc.TemplateInfo, opts.NegationPenalty)
			score *= penalty
		}

		// Skip results below threshold
		if score < opts.MinScore {
			continue
		}

		results = append(results, SearchResult{
			Document:      doc,
			Score:         score,
			SemanticScore: semanticScore,
			KeywordScore:  keywordScore,
			PackageScore:  packageScore,
		})
	}

	// Sort by score descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	// Limit to TopK
	if opts.TopK > 0 && len(results) > opts.TopK {
		results = results[:opts.TopK]
	}

	return results
}

// cosineSimilarity calculates cosine similarity between two vectors.
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0.0
	}

	var dotProduct float64
	var normA, normB float64

	for i := range a {
		dotProduct += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}

	if normA == 0 || normB == 0 {
		return 0.0
	}

	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}

// calculateKeywordScore calculates keyword overlap between query and template.
func calculateKeywordScore(queryTokens []string, tmpl *template.TemplateInfo) float64 {
	if len(queryTokens) == 0 {
		return 0.0
	}

	keywords := tmpl.GetAllKeywords()
	keywordSet := make(map[string]bool)
	for _, k := range keywords {
		keywordSet[strings.ToLower(k)] = true
	}

	// Also include filename parts as keywords
	nameParts := strings.Split(strings.TrimSuffix(tmpl.FileName, ".yml"), "-")
	for _, p := range nameParts {
		keywordSet[strings.ToLower(p)] = true
	}

	// Add distribution and image type
	keywordSet[strings.ToLower(tmpl.Distribution)] = true
	keywordSet[strings.ToLower(tmpl.ImageType)] = true
	keywordSet[strings.ToLower(tmpl.Architecture)] = true

	matches := 0
	for _, token := range queryTokens {
		if keywordSet[token] {
			matches++
		}
	}

	return float64(matches) / float64(len(queryTokens))
}

// calculatePackageScore calculates package matching between query and template.
func calculatePackageScore(queryPackages []string, tmpl *template.TemplateInfo) float64 {
	if len(queryPackages) == 0 {
		return 0.0
	}

	packageSet := tmpl.GetPackageSet()
	// Normalize package set to lowercase
	normalizedSet := make(map[string]bool)
	for pkg := range packageSet {
		normalizedSet[strings.ToLower(pkg)] = true
	}

	matches := 0
	for _, pkg := range queryPackages {
		// Check exact match
		if normalizedSet[pkg] {
			matches++
			continue
		}
		// Check prefix match (e.g., "docker" matches "docker-ce")
		for tmplPkg := range normalizedSet {
			if strings.HasPrefix(tmplPkg, pkg) || strings.HasPrefix(pkg, tmplPkg) {
				matches++
				break
			}
		}
	}

	return float64(matches) / float64(len(queryPackages))
}

// calculateNegationPenalty calculates penalty for templates containing excluded terms.
func calculateNegationPenalty(negativeTerms []string, tmpl *template.TemplateInfo, basePenalty float64) float64 {
	if len(negativeTerms) == 0 {
		return 1.0 // No penalty
	}

	// Check packages
	packageSet := tmpl.GetPackageSet()
	for _, term := range negativeTerms {
		for pkg := range packageSet {
			if strings.Contains(strings.ToLower(pkg), term) {
				return basePenalty // Apply penalty
			}
		}
	}

	// Check keywords
	keywords := tmpl.GetAllKeywords()
	for _, term := range negativeTerms {
		for _, kw := range keywords {
			if strings.Contains(strings.ToLower(kw), term) {
				return basePenalty
			}
		}
	}

	return 1.0 // No penalty
}

// GetDocuments returns all documents in the index (for iteration).
func (idx *Index) GetDocuments() []*Document {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	docs := make([]*Document, len(idx.documents))
	copy(docs, idx.documents)
	return docs
}
