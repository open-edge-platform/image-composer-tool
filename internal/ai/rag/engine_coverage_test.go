package rag

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/ai"
)

// TestNewEngineWithOllama tests engine creation with Ollama provider.
func TestNewEngineWithOllama(t *testing.T) {
	config := ai.DefaultConfig()
	config.Provider = ai.ProviderOllama
	config.Ollama.BaseURL = "http://localhost:11434"
	config.Ollama.EmbeddingModel = "nomic-embed-text"
	config.Ollama.Model = "llama3.1:8b"
	config.Cache.Enabled = false

	engine, err := NewEngine(config)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	if engine == nil {
		t.Fatal("engine should not be nil")
	}

	if engine.initialized {
		t.Error("engine should not be initialized yet")
	}
}

// TestNewEngineWithOpenAI tests engine creation with OpenAI provider fails without key.
func TestNewEngineWithOpenAI(t *testing.T) {
	// Ensure no API key
	t.Setenv("OPENAI_API_KEY", "")

	config := ai.DefaultConfig()
	config.Provider = ai.ProviderOpenAI

	_, err := NewEngine(config)
	if err == nil {
		t.Error("expected error when OPENAI_API_KEY is not set")
	}
}

// TestNewEngineWithOpenAISuccess tests engine creation with OpenAI provider with key.
func TestNewEngineWithOpenAISuccess(t *testing.T) {
	// Set a test API key
	t.Setenv("OPENAI_API_KEY", "test-key")

	config := ai.DefaultConfig()
	config.Provider = ai.ProviderOpenAI
	config.Cache.Enabled = false

	engine, err := NewEngine(config)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	if engine == nil {
		t.Fatal("engine should not be nil")
	}
}

// TestNewEngineWithCache tests engine creation with cache enabled.
func TestNewEngineWithCache(t *testing.T) {
	tmpDir := t.TempDir()

	config := ai.DefaultConfig()
	config.Provider = ai.ProviderOllama
	config.Cache.Enabled = true
	config.Cache.Dir = tmpDir

	engine, err := NewEngine(config)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	if engine.cache == nil {
		t.Error("cache should be initialized")
	}
}

// TestSearchWithoutInitialize tests that Search fails if not initialized.
func TestSearchWithoutInitialize(t *testing.T) {
	config := ai.DefaultConfig()
	config.Provider = ai.ProviderOllama
	config.Cache.Enabled = false

	engine, err := NewEngine(config)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	_, err = engine.Search(context.Background(), "test query")
	if err == nil {
		t.Error("expected error when engine not initialized")
	}
}

// TestGenerateWithoutInitialize tests that Generate fails if not initialized.
func TestGenerateWithoutInitialize(t *testing.T) {
	config := ai.DefaultConfig()
	config.Provider = ai.ProviderOllama
	config.Cache.Enabled = false

	engine, err := NewEngine(config)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	_, err = engine.Generate(context.Background(), "test query")
	if err == nil {
		t.Error("expected error when engine not initialized")
	}
}

// TestInitializeWithNoTemplates tests Initialize fails with empty templates dir.
func TestInitializeWithNoTemplates(t *testing.T) {
	tmpDir := t.TempDir()

	config := ai.DefaultConfig()
	config.Provider = ai.ProviderOllama
	config.Cache.Enabled = false
	config.TemplatesDir = tmpDir

	engine, err := NewEngine(config)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	err = engine.Initialize(context.Background())
	if err == nil {
		t.Error("expected error when no templates found")
	}
}

// TestInitializeWithInvalidDir tests Initialize fails with invalid templates dir.
func TestInitializeWithInvalidDir(t *testing.T) {
	config := ai.DefaultConfig()
	config.Provider = ai.ProviderOllama
	config.Cache.Enabled = false
	config.TemplatesDir = "/nonexistent/path/templates"

	engine, err := NewEngine(config)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	err = engine.Initialize(context.Background())
	if err == nil {
		t.Error("expected error with invalid templates directory")
	}
}

// TestGetStats tests GetStats returns correct statistics.
func TestGetStats(t *testing.T) {
	config := ai.DefaultConfig()
	config.Provider = ai.ProviderOllama
	config.Cache.Enabled = false

	engine, err := NewEngine(config)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	stats := engine.GetStats()

	if stats.Initialized {
		t.Error("stats should show not initialized")
	}

	if stats.Provider != string(ai.ProviderOllama) {
		t.Errorf("expected provider 'ollama', got '%s'", stats.Provider)
	}

	if stats.CacheEnabled {
		t.Error("cache should be disabled")
	}
}

// TestClearCache tests ClearCache works correctly.
func TestClearCache(t *testing.T) {
	// Test with no cache
	config := ai.DefaultConfig()
	config.Provider = ai.ProviderOllama
	config.Cache.Enabled = false

	engine, err := NewEngine(config)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	err = engine.ClearCache()
	if err != nil {
		t.Errorf("ClearCache should not error when cache is nil: %v", err)
	}

	// Test with cache
	tmpDir := t.TempDir()
	config.Cache.Enabled = true
	config.Cache.Dir = tmpDir

	engine2, err := NewEngine(config)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	err = engine2.ClearCache()
	if err != nil {
		t.Errorf("ClearCache failed: %v", err)
	}
}

// TestGetStatsWithCache tests GetStats includes cache stats.
func TestGetStatsWithCache(t *testing.T) {
	tmpDir := t.TempDir()

	config := ai.DefaultConfig()
	config.Provider = ai.ProviderOllama
	config.Cache.Enabled = true
	config.Cache.Dir = tmpDir

	engine, err := NewEngine(config)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	stats := engine.GetStats()

	if !stats.CacheEnabled {
		t.Error("stats should show cache enabled")
	}

	if stats.CacheStats == nil {
		t.Error("cache stats should not be nil")
	}
}

// TestParseQueryEdgeCases tests parseQuery with edge cases.
func TestParseQueryEdgeCases(t *testing.T) {
	tests := []struct {
		name   string
		query  string
		tokens int
	}{
		{
			name:   "empty query",
			query:  "",
			tokens: 0,
		},
		{
			name:   "only stop words",
			query:  "a the is are and or",
			tokens: 0,
		},
		{
			name:   "punctuation handling",
			query:  "create, edge. image!",
			tokens: 2, // "edge" and "image" should remain after stop word filtering (create is a stop word)
		},
		{
			name:   "negation at end",
			query:  "cloud image without",
			tokens: 2, // cloud, image (without is negation keyword)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens, _, _ := parseQuery(tt.query)
			if len(tokens) != tt.tokens {
				t.Errorf("expected %d tokens, got %d: %v", tt.tokens, len(tokens), tokens)
			}
		})
	}
}

// TestCleanYAMLResponseEdgeCases tests cleanYAMLResponse with edge cases.
func TestCleanYAMLResponseEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty input",
			input:    "",
			expected: "",
		},
		{
			name:     "only whitespace",
			input:    "   \n\n  ",
			expected: "",
		},
		{
			name:     "nested code blocks",
			input:    "```yaml\n```yaml\ninner\n```\n```",
			expected: "inner",
		},
		{
			name:     "code block at start",
			input:    "```yaml\nimage: test\n```",
			expected: "image: test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cleanYAMLResponse(tt.input)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

// TestIsStopWordComprehensive tests isStopWord with more words.
func TestIsStopWordComprehensive(t *testing.T) {
	stopWords := []string{
		"a", "an", "the", "is", "are", "was", "were",
		"i", "me", "my", "we", "our", "you", "your",
		"want", "need", "create", "make", "build",
		"please", "can", "help",
	}

	nonStopWords := []string{
		"docker", "nginx", "cloud", "edge", "minimal",
		"kubernetes", "container", "server", "image",
	}

	for _, word := range stopWords {
		if !isStopWord(word) {
			t.Errorf("expected '%s' to be a stop word", word)
		}
	}

	for _, word := range nonStopWords {
		if isStopWord(word) {
			t.Errorf("expected '%s' to NOT be a stop word", word)
		}
	}
}

// TestNewEngineWithCacheError tests engine creation fails with invalid cache dir.
func TestNewEngineWithCacheError(t *testing.T) {
	config := ai.DefaultConfig()
	config.Provider = ai.ProviderOllama
	config.Cache.Enabled = true
	config.Cache.Dir = "/root/nonexistent/cache" // Should fail for non-root

	// This might succeed if running as root, so check euid
	if os.Geteuid() == 0 {
		t.Skip("Skipping test when running as root")
	}

	_, err := NewEngine(config)
	if err == nil {
		t.Error("expected error with invalid cache directory")
	}
}

// TestEngineConfigRetention tests that engine retains config properly.
func TestEngineConfigRetention(t *testing.T) {
	config := ai.DefaultConfig()
	config.Provider = ai.ProviderOllama
	config.Ollama.BaseURL = "http://custom:11434"
	config.Ollama.EmbeddingModel = "custom-embed"
	config.Ollama.Model = "custom-chat"
	config.Cache.Enabled = false
	config.Scoring.Semantic = 0.6
	config.Scoring.Keyword = 0.3
	config.Scoring.Package = 0.1

	engine, err := NewEngine(config)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	if engine.config.Scoring.Semantic != 0.6 {
		t.Errorf("semantic weight not retained: got %f", engine.config.Scoring.Semantic)
	}

	if engine.config.Scoring.Keyword != 0.3 {
		t.Errorf("keyword weight not retained: got %f", engine.config.Scoring.Keyword)
	}
}

// TestParseQueryWithMultipleNegations tests negation parsing.
func TestParseQueryWithMultipleNegations(t *testing.T) {
	query := "edge image without docker and without nginx"
	_, _, negatives := parseQuery(query)

	hasDocker := false
	hasNginx := false
	for _, neg := range negatives {
		if neg == "docker" {
			hasDocker = true
		}
		if neg == "nginx" {
			hasNginx = true
		}
	}

	if !hasDocker {
		t.Error("expected 'docker' in negative terms")
	}
	if !hasNginx {
		t.Error("expected 'nginx' in negative terms")
	}
}

// TestParseQueryWithPackagePatterns tests package pattern detection.
func TestParseQueryWithPackagePatterns(t *testing.T) {
	query := "create image with docker and nginx and redis"
	_, packages, _ := parseQuery(query)

	if len(packages) < 3 {
		t.Errorf("expected at least 3 packages, got %d: %v", len(packages), packages)
	}

	hasDocker := false
	hasNginx := false
	hasRedis := false
	for _, pkg := range packages {
		if pkg == "docker" {
			hasDocker = true
		}
		if pkg == "nginx" {
			hasNginx = true
		}
		if pkg == "redis" {
			hasRedis = true
		}
	}

	if !hasDocker {
		t.Error("expected 'docker' in packages")
	}
	if !hasNginx {
		t.Error("expected 'nginx' in packages")
	}
	if !hasRedis {
		t.Error("expected 'redis' in packages")
	}
}

// TestInitializeWithValidTemplates creates valid templates and initializes.
func TestInitializeWithValidTemplates(t *testing.T) {
	// This test requires a running Ollama server, so skip if not available
	t.Skip("Requires running Ollama server")

	tmpDir := t.TempDir()

	// Create a valid template file
	templateContent := `image:
  name: test-template
  version: "1.0.0"
target:
  os: azure-linux
  dist: azl3
  arch: x86_64
  imageType: raw
systemConfig:
  packages:
    - vim
`
	templatePath := filepath.Join(tmpDir, "test.yml")
	if err := os.WriteFile(templatePath, []byte(templateContent), 0644); err != nil {
		t.Fatalf("failed to write template: %v", err)
	}

	config := ai.DefaultConfig()
	config.Provider = ai.ProviderOllama
	config.Cache.Enabled = false
	config.TemplatesDir = tmpDir
	config.Ollama.Timeout = 30

	engine, err := NewEngine(config)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	err = engine.Initialize(context.Background())
	if err != nil {
		t.Logf("Initialize failed (expected if Ollama not running): %v", err)
	}
}
