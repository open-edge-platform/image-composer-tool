// Package index provides in-memory vector index with cosine similarity search.
package index

import (
	"math"
	"sort"
	"strings"
	"sync"
)

// Item is anything the index can rank: a template, a bundle, or a package.
// Defined here to keep the index package free of concrete-type imports.
type Item interface {
	ID() string             // stable identity (filename, bundle id, package name)
	Keywords() []string     // keyword-overlap scoring input
	PackageNames() []string // package-overlap + negation scoring input
	SearchableText() string // text that was embedded
}

// Document represents an indexed item with its embedding.
type Document struct {
	// Item is the indexed item (template, bundle, or package)
	Item Item

	// Embedding is the vector representation
	Embedding []float32

	// ContentHash is the hash of the item content
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
		keywordScore := calculateKeywordScore(normalizedTokens, doc.Item)

		// Calculate package score
		packageScore := calculatePackageScore(normalizedPackages, doc.Item)

		// Calculate combined score
		score := (opts.SemanticWeight * semanticScore) +
			(opts.KeywordWeight * keywordScore) +
			(opts.PackageWeight * packageScore)

		// Apply negation penalty
		if len(negativeLower) > 0 {
			penalty := calculateNegationPenalty(negativeLower, doc.Item, opts.NegationPenalty)
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

// calculateKeywordScore calculates keyword overlap between query and item.
func calculateKeywordScore(queryTokens []string, item Item) float64 {
	if len(queryTokens) == 0 || item == nil {
		return 0.0
	}

	keywords := item.Keywords()
	keywordSet := make(map[string]bool)
	for _, k := range keywords {
		keywordSet[strings.ToLower(k)] = true
	}

	// Also include ID parts as keywords (e.g. filename parts or bundle id parts)
	idParts := strings.Split(strings.TrimSuffix(strings.TrimSuffix(item.ID(), ".yaml"), ".yml"), "-")
	for _, p := range idParts {
		if p != "" {
			keywordSet[strings.ToLower(p)] = true
		}
	}

	matches := 0
	for _, token := range queryTokens {
		if keywordSet[token] {
			matches++
		}
	}

	return float64(matches) / float64(len(queryTokens))
}

// calculatePackageScore calculates package matching between query and item.
func calculatePackageScore(queryPackages []string, item Item) float64 {
	if len(queryPackages) == 0 || item == nil {
		return 0.0
	}

	packageNames := item.PackageNames()
	normalizedSet := make(map[string]bool)
	for _, pkg := range packageNames {
		normalizedSet[strings.ToLower(pkg)] = true
		// Also add base name without version suffixes
		if idx := strings.Index(pkg, "="); idx > 0 {
			normalizedSet[strings.ToLower(pkg[:idx])] = true
		}
	}

	matches := 0
	for _, pkg := range queryPackages {
		// Check exact match
		if normalizedSet[pkg] {
			matches++
			continue
		}
		// Check prefix match (e.g., "docker" matches "docker-ce")
		for itemPkg := range normalizedSet {
			if strings.HasPrefix(itemPkg, pkg) || strings.HasPrefix(pkg, itemPkg) {
				matches++
				break
			}
		}
	}

	return float64(matches) / float64(len(queryPackages))
}

// calculateNegationPenalty calculates penalty for items containing excluded terms.
func calculateNegationPenalty(negativeTerms []string, item Item, basePenalty float64) float64 {
	if len(negativeTerms) == 0 || item == nil {
		return 1.0 // No penalty
	}

	// Check packages
	for _, term := range negativeTerms {
		for _, pkg := range item.PackageNames() {
			if strings.Contains(strings.ToLower(pkg), term) {
				return basePenalty // Apply penalty
			}
		}
	}

	// Check keywords
	for _, term := range negativeTerms {
		for _, kw := range item.Keywords() {
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
