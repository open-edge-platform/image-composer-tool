// Package rag provides the RAG (Retrieval-Augmented Generation) engine.
package rag

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/open-edge-platform/image-composer-tool/internal/ai"
	"github.com/open-edge-platform/image-composer-tool/internal/ai/cache"
	"github.com/open-edge-platform/image-composer-tool/internal/ai/index"
	"github.com/open-edge-platform/image-composer-tool/internal/ai/provider"
	"github.com/open-edge-platform/image-composer-tool/internal/ai/template"
)

// Engine is the main RAG engine that orchestrates indexing and search.
type Engine struct {
	config        ai.Config
	embedProvider provider.EmbeddingProvider
	chatProvider  provider.ChatProvider
	cache         *cache.Cache
	index         *index.Index
	templatesDir  string
	initialized   bool
	indexedAt     time.Time
	templateCount int
}

// NewEngine creates a new RAG engine with the given configuration.
func NewEngine(config ai.Config) (*Engine, error) {
	// Create embedding provider based on config
	var embedProvider provider.EmbeddingProvider
	var chatProvider provider.ChatProvider

	switch config.Provider {
	case ai.ProviderOpenAI:
		openaiProvider, err := provider.NewOpenAIProvider(
			config.OpenAI.EmbeddingModel,
			config.OpenAI.Model,
			config.GetTimeout(),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create OpenAI provider: %w", err)
		}
		embedProvider = openaiProvider
		chatProvider = openaiProvider
	default:
		ollamaProvider := provider.NewOllamaProvider(
			config.Ollama.BaseURL,
			config.Ollama.EmbeddingModel,
			config.Ollama.Model,
			config.GetTimeout(),
		)
		embedProvider = ollamaProvider
		chatProvider = ollamaProvider
	}

	// Create cache if enabled
	var embeddingCache *cache.Cache
	if config.Cache.Enabled {
		var err error
		embeddingCache, err = cache.NewCache(config.Cache.Dir)
		if err != nil {
			return nil, fmt.Errorf("failed to create cache: %w", err)
		}
	}

	return &Engine{
		config:        config,
		embedProvider: embedProvider,
		chatProvider:  chatProvider,
		cache:         embeddingCache,
		index:         index.NewIndex(),
		templatesDir:  config.TemplatesDir,
	}, nil
}

// Initialize loads templates and builds the index.
func (e *Engine) Initialize(ctx context.Context) error {
	// Scan templates
	templates, err := template.ScanTemplates(e.templatesDir)
	if err != nil {
		return fmt.Errorf("failed to scan templates: %w", err)
	}

	if len(templates) == 0 {
		return fmt.Errorf("no templates found in %s", e.templatesDir)
	}

	// Clear existing index
	e.index.Clear()

	// Index each template
	modelID := e.embedProvider.ModelID()
	dimensions := e.embedProvider.Dimensions()

	for _, tmpl := range templates {
		// Compute content hash
		contentHash := cache.ComputeContentHash(tmpl.RawContent)

		// Try to get embedding from cache
		var embedding []float32
		if e.cache != nil {
			if cached, ok := e.cache.Get(contentHash, modelID); ok {
				embedding = cached
			}
		}

		// Generate embedding if not cached
		if embedding == nil {
			searchableText := tmpl.BuildSearchableText()
			var err error
			embedding, err = e.embedProvider.Embed(ctx, searchableText)
			if err != nil {
				return fmt.Errorf("failed to generate embedding for %s: %w", tmpl.FileName, err)
			}

			// Store in cache
			if e.cache != nil {
				if err := e.cache.Put(contentHash, modelID, tmpl.FileName, dimensions, embedding); err != nil {
					// Log warning but continue
					fmt.Printf("Warning: failed to cache embedding for %s: %v\n", tmpl.FileName, err)
				}
			}
		}

		// Add to index
		doc := &index.Document{
			TemplateInfo:   tmpl,
			Embedding:      embedding,
			SearchableText: tmpl.BuildSearchableText(),
			ContentHash:    contentHash,
		}
		e.index.Add(doc)
	}

	e.initialized = true
	e.indexedAt = time.Now()
	e.templateCount = len(templates)

	return nil
}

// SearchResult represents a template search result.
type SearchResult struct {
	// Template is the matched template
	Template *template.TemplateInfo

	// Score is the combined relevance score
	Score float64

	// SemanticScore is the semantic similarity score
	SemanticScore float64

	// KeywordScore is the keyword overlap score
	KeywordScore float64

	// PackageScore is the package matching score
	PackageScore float64
}

// Search finds templates matching the query.
func (e *Engine) Search(ctx context.Context, query string) ([]SearchResult, error) {
	if !e.initialized {
		return nil, fmt.Errorf("engine not initialized, call Initialize first")
	}

	// Generate query embedding
	queryEmbedding, err := e.embedProvider.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to generate query embedding: %w", err)
	}

	// Extract tokens and packages from query
	tokens, packages, negativeTerms := parseQuery(query)

	// Configure search options
	opts := index.SearchOptions{
		TopK:            5,
		SemanticWeight:  e.config.Scoring.Semantic,
		KeywordWeight:   e.config.Scoring.Keyword,
		PackageWeight:   e.config.Scoring.Package,
		MinScore:        e.config.Scoring.MinScoreThreshold,
		NegativeTerms:   negativeTerms,
		NegationPenalty: e.config.Classification.NegationPenalty,
	}

	// Perform search
	indexResults := e.index.Search(queryEmbedding, tokens, packages, opts)

	// Convert to SearchResult
	results := make([]SearchResult, len(indexResults))
	for i, ir := range indexResults {
		results[i] = SearchResult{
			Template:      ir.Document.TemplateInfo,
			Score:         ir.Score,
			SemanticScore: ir.SemanticScore,
			KeywordScore:  ir.KeywordScore,
			PackageScore:  ir.PackageScore,
		}
	}

	return results, nil
}

// Generate generates a template based on the query and retrieved context.
func (e *Engine) Generate(ctx context.Context, query string) (string, error) {
	if !e.initialized {
		return "", fmt.Errorf("engine not initialized, call Initialize first")
	}

	// Search for relevant templates
	results, err := e.Search(ctx, query)
	if err != nil {
		return "", err
	}

	if len(results) == 0 {
		return "", fmt.Errorf("no relevant templates found for query")
	}

	// Build context from top results
	var contextBuilder strings.Builder
	contextBuilder.WriteString("You are an expert at generating OS image YAML templates for image-composer-tool.\n")
	contextBuilder.WriteString("Here are some example templates to use as reference:\n\n")

	for i, result := range results {
		if i >= 3 { // Limit to top 3 examples
			break
		}
		contextBuilder.WriteString(fmt.Sprintf("--- Example %d: %s (score: %.2f) ---\n", i+1, result.Template.FileName, result.Score))
		contextBuilder.WriteString(string(result.Template.RawContent))
		contextBuilder.WriteString("\n\n")
	}

	contextBuilder.WriteString("Based on these examples, generate a YAML template for the following request:\n")
	contextBuilder.WriteString(query)
	contextBuilder.WriteString("\n\nGenerate only the YAML template, no explanation.")

	messages := []provider.ChatMessage{
		{Role: "system", Content: "You are an expert at generating OS image YAML templates. Generate valid YAML based on the provided examples and user request."},
		{Role: "user", Content: contextBuilder.String()},
	}

	response, err := e.chatProvider.Chat(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("failed to generate template: %w", err)
	}

	// Clean up response - extract YAML if wrapped in code blocks
	response = cleanYAMLResponse(response)

	return response, nil
}

// Stats returns engine statistics.
type Stats struct {
	Initialized    bool
	IndexedAt      time.Time
	TemplateCount  int
	Provider       string
	EmbeddingModel string
	CacheEnabled   bool
	CacheStats     *cache.CacheStats
}

// GetStats returns engine statistics.
func (e *Engine) GetStats() Stats {
	stats := Stats{
		Initialized:    e.initialized,
		IndexedAt:      e.indexedAt,
		TemplateCount:  e.templateCount,
		Provider:       string(e.config.Provider),
		EmbeddingModel: e.embedProvider.ModelID(),
		CacheEnabled:   e.cache != nil,
	}

	if e.cache != nil {
		cacheStats := e.cache.Stats()
		stats.CacheStats = &cacheStats
	}

	return stats
}

// ClearCache clears the embedding cache.
func (e *Engine) ClearCache() error {
	if e.cache == nil {
		return nil
	}
	return e.cache.Clear()
}

// parseQuery extracts tokens, packages, and negative terms from a query.
func parseQuery(query string) (tokens []string, packages []string, negativeTerms []string) {
	// Common negation keywords
	negationKeywords := []string{"without", "no", "exclude", "not"}

	// Known package patterns
	packagePatterns := []string{
		"docker", "nginx", "apache", "mysql", "postgres", "redis",
		"nodejs", "python", "golang", "rust", "java",
		"curl", "wget", "git", "vim", "nano",
		"openssh", "cloud-init", "systemd",
	}

	words := strings.Fields(strings.ToLower(query))

	inNegation := false
	for i, word := range words {
		// Clean punctuation
		word = strings.Trim(word, ".,!?;:")

		// Check for negation keywords
		isNegation := false
		for _, neg := range negationKeywords {
			if word == neg {
				isNegation = true
				inNegation = true
				break
			}
		}

		if isNegation {
			continue
		}

		// If following a negation keyword, it's a negative term
		if inNegation {
			negativeTerms = append(negativeTerms, word)
			// Reset negation after capturing the term
			if i > 0 && i < len(words)-1 {
				// Check if next word could also be negated (e.g., "without docker and nginx")
				nextWord := strings.Trim(words[i+1], ".,!?;:")
				if nextWord != "and" && nextWord != "or" {
					inNegation = false
				}
			} else {
				inNegation = false
			}
			continue
		}

		// Check if it's a known package
		isPackage := false
		for _, pkg := range packagePatterns {
			if strings.Contains(word, pkg) {
				packages = append(packages, word)
				isPackage = true
				break
			}
		}

		// Add as token if not a common stop word
		if !isStopWord(word) {
			tokens = append(tokens, word)
			// Package was already added above, no additional action needed
			_ = isPackage
		}
	}

	return tokens, packages, negativeTerms
}

// isStopWord returns true if the word is a common stop word.
func isStopWord(word string) bool {
	stopWords := map[string]bool{
		"a": true, "an": true, "the": true, "is": true, "are": true,
		"was": true, "were": true, "be": true, "been": true, "being": true,
		"have": true, "has": true, "had": true, "do": true, "does": true,
		"did": true, "will": true, "would": true, "could": true, "should": true,
		"may": true, "might": true, "must": true, "shall": true,
		"i": true, "me": true, "my": true, "we": true, "our": true,
		"you": true, "your": true, "he": true, "she": true, "it": true,
		"they": true, "them": true, "their": true,
		"this": true, "that": true, "these": true, "those": true,
		"and": true, "or": true, "but": true, "if": true, "then": true,
		"for": true, "of": true, "to": true, "in": true, "on": true,
		"at": true, "by": true, "with": true, "from": true, "as": true,
		"into": true, "through": true, "during": true, "before": true,
		"after": true, "above": true, "below": true, "up": true, "down": true,
		"want": true, "need": true, "create": true, "make": true, "build": true,
		"please": true, "can": true, "help": true,
	}
	return stopWords[word]
}

// cleanYAMLResponse removes code block markers from LLM response.
func cleanYAMLResponse(response string) string {
	lines := strings.Split(response, "\n")
	var cleaned []string
	inCodeBlock := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```yaml") || strings.HasPrefix(trimmed, "```yml") {
			inCodeBlock = true
			continue
		}
		if trimmed == "```" {
			inCodeBlock = false
			continue
		}
		if inCodeBlock || !strings.HasPrefix(trimmed, "```") {
			cleaned = append(cleaned, line)
		}
	}

	return strings.TrimSpace(strings.Join(cleaned, "\n"))
}
