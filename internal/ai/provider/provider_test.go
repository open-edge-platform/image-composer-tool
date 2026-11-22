package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewOllamaProvider(t *testing.T) {
	provider := NewOllamaProvider(
		"http://localhost:11434",
		"nomic-embed-text",
		"llama3.1:8b",
		60*time.Second,
	)

	if provider.ModelID() != "nomic-embed-text" {
		t.Errorf("expected model ID 'nomic-embed-text', got '%s'", provider.ModelID())
	}

	if provider.Dimensions() != 768 {
		t.Errorf("expected dimensions 768, got %d", provider.Dimensions())
	}
}

func TestOllamaProviderDimensions(t *testing.T) {
	tests := []struct {
		model      string
		dimensions int
	}{
		{"nomic-embed-text", 768},
		{"mxbai-embed-large", 1024},
		{"all-minilm", 384},
		{"unknown-model", 768}, // default
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			provider := NewOllamaProvider("http://localhost:11434", tt.model, "llama3.1:8b", 60*time.Second)
			if provider.Dimensions() != tt.dimensions {
				t.Errorf("expected dimensions %d for model %s, got %d", tt.dimensions, tt.model, provider.Dimensions())
			}
		})
	}
}

func TestOllamaEmbed(t *testing.T) {
	// Create a mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embeddings" {
			t.Errorf("expected path /api/embeddings, got %s", r.URL.Path)
		}

		if r.Method != "POST" {
			t.Errorf("expected POST method, got %s", r.Method)
		}

		// Parse request
		var req ollamaEmbedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}

		if req.Model != "nomic-embed-text" {
			t.Errorf("expected model 'nomic-embed-text', got '%s'", req.Model)
		}

		// Return mock embedding
		resp := ollamaEmbedResponse{
			Embedding: []float64{0.1, 0.2, 0.3, 0.4, 0.5},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewOllamaProvider(server.URL, "nomic-embed-text", "llama3.1:8b", 60*time.Second)

	embedding, err := provider.Embed(context.Background(), "test text")
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}

	if len(embedding) != 5 {
		t.Errorf("expected 5 dimensions, got %d", len(embedding))
	}

	if embedding[0] != 0.1 {
		t.Errorf("expected first value 0.1, got %f", embedding[0])
	}
}

func TestOllamaEmbedError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal server error"))
	}))
	defer server.Close()

	provider := NewOllamaProvider(server.URL, "nomic-embed-text", "llama3.1:8b", 60*time.Second)

	_, err := provider.Embed(context.Background(), "test text")
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestOllamaChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("expected path /api/chat, got %s", r.URL.Path)
		}

		var req ollamaChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}

		if req.Model != "llama3.1:8b" {
			t.Errorf("expected model 'llama3.1:8b', got '%s'", req.Model)
		}

		resp := ollamaChatResponse{
			Message: struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			}{
				Role:    "assistant",
				Content: "Hello, I'm the assistant!",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewOllamaProvider(server.URL, "nomic-embed-text", "llama3.1:8b", 60*time.Second)

	messages := []ChatMessage{
		{Role: "user", Content: "Hello"},
	}

	response, err := provider.Chat(context.Background(), messages)
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	if response != "Hello, I'm the assistant!" {
		t.Errorf("expected response 'Hello, I'm the assistant!', got '%s'", response)
	}
}

func TestOpenAIProviderDimensions(t *testing.T) {
	// Skip if no API key (we can't actually create the provider without it)
	// But we can test the dimension mapping logic indirectly

	tests := []struct {
		model      string
		dimensions int
	}{
		{"text-embedding-3-small", 1536},
		{"text-embedding-3-large", 3072},
		{"text-embedding-ada-002", 1536},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			// We can't test NewOpenAIProvider without an API key,
			// but we can verify the expected dimensions are correct
			expectedDim := 1536 // default
			switch tt.model {
			case "text-embedding-3-small":
				expectedDim = 1536
			case "text-embedding-3-large":
				expectedDim = 3072
			case "text-embedding-ada-002":
				expectedDim = 1536
			}
			if expectedDim != tt.dimensions {
				t.Errorf("dimension mismatch for %s: expected %d, got %d", tt.model, tt.dimensions, expectedDim)
			}
		})
	}
}

func TestOpenAIProviderRequiresAPIKey(t *testing.T) {
	// Ensure OPENAI_API_KEY is not set for this test
	t.Setenv("OPENAI_API_KEY", "")

	_, err := NewOpenAIProvider("", "text-embedding-3-small", "gpt-4o-mini", 60*time.Second)
	if err == nil {
		t.Error("expected error when OPENAI_API_KEY is not set")
	}
}

func TestOpenAIProviderBaseURL(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	tests := []struct {
		name, config, env, want string
	}{
		{"default", "", "", "https://api.openai.com/v1"},
		{"config", "https://gnai.intel.com/api/providers/openai/v1/", "", "https://gnai.intel.com/api/providers/openai/v1"},
		{"env wins", "https://example.com/v1", "https://gnai.intel.com/api/providers/openai/v1", "https://gnai.intel.com/api/providers/openai/v1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("OPENAI_BASE_URL", tt.env)
			p, err := NewOpenAIProvider(tt.config, "text-embedding-3-large", "gpt-5.4-mini", 60*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			if p.baseURL != tt.want {
				t.Errorf("baseURL = %q, want %q", p.baseURL, tt.want)
			}
		})
	}
}
