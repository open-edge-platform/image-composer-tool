// Package ai provides AI-powered template generation using Template-Enriched RAG.
package ai

import (
	"time"
)

// DefaultOllamaBaseURL is the default URL for local Ollama server.
const DefaultOllamaBaseURL = "http://localhost:11434"

// DefaultOllamaModel is the default chat model for Ollama.
const DefaultOllamaModel = "llama3.1:8b"

// DefaultOllamaEmbeddingModel is the default embedding model for Ollama.
const DefaultOllamaEmbeddingModel = "nomic-embed-text"

// DefaultOpenAIModel is the default chat model for OpenAI.
const DefaultOpenAIModel = "gpt-4o-mini"

// DefaultOpenAIEmbeddingModel is the default embedding model for OpenAI.
const DefaultOpenAIEmbeddingModel = "text-embedding-3-small"

// DefaultTemplatesDir is the default directory for templates.
const DefaultTemplatesDir = "./image-templates"

// DefaultCacheDir is the default directory for AI cache.
const DefaultCacheDir = "./.ai-cache"

// ProviderType represents the AI provider type.
type ProviderType string

const (
	// ProviderOllama represents the Ollama provider (local, free).
	ProviderOllama ProviderType = "ollama"
	// ProviderOpenAI represents the OpenAI provider (cloud, requires API key).
	ProviderOpenAI ProviderType = "openai"
)

// Config holds all AI-related configuration.
type Config struct {
	// Provider specifies which AI provider to use: "ollama" or "openai"
	Provider ProviderType `yaml:"provider"`

	// TemplatesDir is the directory containing template YAML files to index
	TemplatesDir string `yaml:"templates_dir"`

	// Ollama holds Ollama-specific settings
	Ollama OllamaConfig `yaml:"ollama"`

	// OpenAI holds OpenAI-specific settings
	OpenAI OpenAIConfig `yaml:"openai"`

	// Cache holds embedding cache settings
	Cache CacheConfig `yaml:"cache"`

	// Conversation holds conversation/session settings
	Conversation ConversationConfig `yaml:"conversation"`

	// Scoring holds hybrid scoring weights
	Scoring ScoringConfig `yaml:"scoring"`

	// Classification holds query classification settings
	Classification ClassificationConfig `yaml:"classification"`
}

// OllamaConfig holds Ollama-specific configuration.
type OllamaConfig struct {
	// BaseURL is the Ollama server URL
	BaseURL string `yaml:"base_url"`

	// Model is the chat model for generation
	Model string `yaml:"model"`

	// EmbeddingModel is the model for embeddings
	EmbeddingModel string `yaml:"embedding_model"`

	// Timeout is the request timeout in seconds
	Timeout int `yaml:"timeout"`
}

// OpenAIConfig holds OpenAI-specific configuration.
type OpenAIConfig struct {
	// BaseURL is the API root including /v1, for OpenAI-compatible services
	// such as GNAI. Empty means api.openai.com.
	BaseURL string `yaml:"base_url"`

	// Model is the chat model for generation
	Model string `yaml:"model"`

	// EmbeddingModel is the model for embeddings
	EmbeddingModel string `yaml:"embedding_model"`

	// Timeout is the request timeout in seconds
	Timeout int `yaml:"timeout"`
}

// CacheConfig holds embedding cache configuration.
type CacheConfig struct {
	// Enabled enables/disables embedding cache
	Enabled bool `yaml:"enabled"`

	// Dir is the cache directory path
	Dir string `yaml:"dir"`
}

// ConversationConfig holds conversation/session configuration.
type ConversationConfig struct {
	// MaxHistory is the number of messages to retain in context
	MaxHistory int `yaml:"max_history"`

	// SessionTimeout is the session inactivity timeout
	SessionTimeout time.Duration `yaml:"session_timeout"`
}

// ScoringConfig holds hybrid scoring weights.
type ScoringConfig struct {
	// Semantic is the weight for semantic similarity (default 0.70)
	Semantic float64 `yaml:"semantic"`

	// Keyword is the weight for keyword overlap (default 0.20)
	Keyword float64 `yaml:"keyword"`

	// Package is the weight for package matching (default 0.10)
	Package float64 `yaml:"package"`

	// MinScoreThreshold is the minimum score to include a result
	MinScoreThreshold float64 `yaml:"min_score_threshold"`
}

// ClassificationConfig holds query classification settings.
type ClassificationConfig struct {
	// PackageThreshold is the package count for package-explicit mode
	PackageThreshold int `yaml:"package_threshold"`

	// KeywordDensity is the ratio for keyword-heavy mode
	KeywordDensity float64 `yaml:"keyword_density"`

	// NegationPenalty is the score multiplier for excluded items
	NegationPenalty float64 `yaml:"negation_penalty"`
}

// DefaultConfig returns the default AI configuration.
func DefaultConfig() Config {
	return Config{
		Provider:     ProviderOllama,
		TemplatesDir: DefaultTemplatesDir,
		Ollama: OllamaConfig{
			BaseURL:        DefaultOllamaBaseURL,
			Model:          DefaultOllamaModel,
			EmbeddingModel: DefaultOllamaEmbeddingModel,
			Timeout:        120,
		},
		OpenAI: OpenAIConfig{
			Model:          DefaultOpenAIModel,
			EmbeddingModel: DefaultOpenAIEmbeddingModel,
			Timeout:        60,
		},
		Cache: CacheConfig{
			Enabled: true,
			Dir:     DefaultCacheDir,
		},
		Conversation: ConversationConfig{
			MaxHistory:     20,
			SessionTimeout: 30 * time.Minute,
		},
		Scoring: ScoringConfig{
			Semantic:          0.70,
			Keyword:           0.20,
			Package:           0.10,
			MinScoreThreshold: 0.40,
		},
		Classification: ClassificationConfig{
			PackageThreshold: 2,
			KeywordDensity:   0.5,
			NegationPenalty:  0.5,
		},
	}
}

// Merge merges the provided config with defaults, using defaults for zero values.
func (c Config) Merge(defaults Config) Config {
	merged := c

	if merged.Provider == "" {
		merged.Provider = defaults.Provider
	}
	if merged.TemplatesDir == "" {
		merged.TemplatesDir = defaults.TemplatesDir
	}

	// Merge Ollama config
	if merged.Ollama.BaseURL == "" {
		merged.Ollama.BaseURL = defaults.Ollama.BaseURL
	}
	if merged.Ollama.Model == "" {
		merged.Ollama.Model = defaults.Ollama.Model
	}
	if merged.Ollama.EmbeddingModel == "" {
		merged.Ollama.EmbeddingModel = defaults.Ollama.EmbeddingModel
	}
	if merged.Ollama.Timeout == 0 {
		merged.Ollama.Timeout = defaults.Ollama.Timeout
	}

	// Merge OpenAI config
	if merged.OpenAI.Model == "" {
		merged.OpenAI.Model = defaults.OpenAI.Model
	}
	if merged.OpenAI.EmbeddingModel == "" {
		merged.OpenAI.EmbeddingModel = defaults.OpenAI.EmbeddingModel
	}
	if merged.OpenAI.Timeout == 0 {
		merged.OpenAI.Timeout = defaults.OpenAI.Timeout
	}

	// Merge Cache config
	// Note: Cache.Enabled defaults to false in Go, so we don't override it here.
	// If user explicitly sets it to false, we respect that.
	if merged.Cache.Dir == "" {
		merged.Cache.Dir = defaults.Cache.Dir
	}

	// Merge Conversation config
	if merged.Conversation.MaxHistory == 0 {
		merged.Conversation.MaxHistory = defaults.Conversation.MaxHistory
	}
	if merged.Conversation.SessionTimeout == 0 {
		merged.Conversation.SessionTimeout = defaults.Conversation.SessionTimeout
	}

	// Merge Scoring config
	if merged.Scoring.Semantic == 0 {
		merged.Scoring.Semantic = defaults.Scoring.Semantic
	}
	if merged.Scoring.Keyword == 0 {
		merged.Scoring.Keyword = defaults.Scoring.Keyword
	}
	if merged.Scoring.Package == 0 {
		merged.Scoring.Package = defaults.Scoring.Package
	}
	if merged.Scoring.MinScoreThreshold == 0 {
		merged.Scoring.MinScoreThreshold = defaults.Scoring.MinScoreThreshold
	}

	// Merge Classification config
	if merged.Classification.PackageThreshold == 0 {
		merged.Classification.PackageThreshold = defaults.Classification.PackageThreshold
	}
	if merged.Classification.KeywordDensity == 0 {
		merged.Classification.KeywordDensity = defaults.Classification.KeywordDensity
	}
	if merged.Classification.NegationPenalty == 0 {
		merged.Classification.NegationPenalty = defaults.Classification.NegationPenalty
	}

	return merged
}

// GetEmbeddingModel returns the embedding model based on the provider.
func (c Config) GetEmbeddingModel() string {
	switch c.Provider {
	case ProviderOpenAI:
		return c.OpenAI.EmbeddingModel
	default:
		return c.Ollama.EmbeddingModel
	}
}

// GetChatModel returns the chat model based on the provider.
func (c Config) GetChatModel() string {
	switch c.Provider {
	case ProviderOpenAI:
		return c.OpenAI.Model
	default:
		return c.Ollama.Model
	}
}

// GetTimeout returns the timeout based on the provider.
func (c Config) GetTimeout() time.Duration {
	switch c.Provider {
	case ProviderOpenAI:
		return time.Duration(c.OpenAI.Timeout) * time.Second
	default:
		return time.Duration(c.Ollama.Timeout) * time.Second
	}
}
