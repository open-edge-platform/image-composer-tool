package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestOllamaChatError tests Chat error handling.
func TestOllamaChatError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal server error"))
	}))
	defer server.Close()

	provider := NewOllamaProvider(server.URL, "nomic-embed-text", "llama3.1:8b", 60*time.Second)

	messages := []ChatMessage{
		{Role: "user", Content: "Hello"},
	}

	_, err := provider.Chat(context.Background(), messages)
	if err == nil {
		t.Error("expected error, got nil")
	}
}

// TestOllamaChatInvalidJSON tests Chat with invalid JSON response.
func TestOllamaChatInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not valid json"))
	}))
	defer server.Close()

	provider := NewOllamaProvider(server.URL, "nomic-embed-text", "llama3.1:8b", 60*time.Second)

	messages := []ChatMessage{
		{Role: "user", Content: "Hello"},
	}

	_, err := provider.Chat(context.Background(), messages)
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

// TestOllamaEmbedInvalidJSON tests Embed with invalid JSON response.
func TestOllamaEmbedInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not valid json"))
	}))
	defer server.Close()

	provider := NewOllamaProvider(server.URL, "nomic-embed-text", "llama3.1:8b", 60*time.Second)

	_, err := provider.Embed(context.Background(), "test text")
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

// TestOllamaProviderModelIDChat tests the chat model ID.
func TestOllamaProviderChatModelID(t *testing.T) {
	provider := NewOllamaProvider("http://localhost:11434", "nomic-embed-text", "llama3.1:8b", 60*time.Second)

	// The provider should use the correct model for chat
	if provider.chatModel != "llama3.1:8b" {
		t.Errorf("expected chat model 'llama3.1:8b', got '%s'", provider.chatModel)
	}
}

// TestOllamaChatMultipleMessages tests Chat with multiple messages.
func TestOllamaChatMultipleMessages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ollamaChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}

		// Verify multiple messages were sent
		if len(req.Messages) != 3 {
			t.Errorf("expected 3 messages, got %d", len(req.Messages))
		}

		// Verify stream is false
		if req.Stream {
			t.Error("expected stream to be false")
		}

		resp := ollamaChatResponse{
			Message: struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			}{
				Role:    "assistant",
				Content: "Response to conversation",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewOllamaProvider(server.URL, "nomic-embed-text", "llama3.1:8b", 60*time.Second)

	messages := []ChatMessage{
		{Role: "system", Content: "You are a helpful assistant"},
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi there!"},
	}

	response, err := provider.Chat(context.Background(), messages)
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	if response != "Response to conversation" {
		t.Errorf("unexpected response: %s", response)
	}
}

// TestOllamaEmbedContextCancelled tests Embed with cancelled context.
func TestOllamaEmbedContextCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Delay to allow context cancellation
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	provider := NewOllamaProvider(server.URL, "nomic-embed-text", "llama3.1:8b", 60*time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := provider.Embed(ctx, "test text")
	if err == nil {
		t.Error("expected error with cancelled context")
	}
}

// TestOllamaChatContextCancelled tests Chat with cancelled context.
func TestOllamaChatContextCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	provider := NewOllamaProvider(server.URL, "nomic-embed-text", "llama3.1:8b", 60*time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	messages := []ChatMessage{
		{Role: "user", Content: "Hello"},
	}

	_, err := provider.Chat(ctx, messages)
	if err == nil {
		t.Error("expected error with cancelled context")
	}
}

// TestOllamaEmbedEmptyText tests Embed with empty text.
func TestOllamaEmbedEmptyText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ollamaEmbedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}

		if req.Prompt != "" {
			t.Errorf("expected empty prompt, got '%s'", req.Prompt)
		}

		resp := ollamaEmbedResponse{
			Embedding: []float64{0.0, 0.0, 0.0},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewOllamaProvider(server.URL, "nomic-embed-text", "llama3.1:8b", 60*time.Second)

	embedding, err := provider.Embed(context.Background(), "")
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}

	if len(embedding) != 3 {
		t.Errorf("expected 3 dimensions, got %d", len(embedding))
	}
}

// TestOllamaProviderWithDifferentModels tests dimension detection for different models.
func TestOllamaProviderWithDifferentModels(t *testing.T) {
	tests := []struct {
		embeddingModel string
		chatModel      string
		expectedDim    int
	}{
		{"nomic-embed-text", "llama3.1:8b", 768},
		{"mxbai-embed-large", "llama3.1:8b", 1024},
		{"all-minilm", "llama3.1:8b", 384},
		{"custom-model", "llama3.1:8b", 768}, // default
	}

	for _, tt := range tests {
		t.Run(tt.embeddingModel, func(t *testing.T) {
			provider := NewOllamaProvider("http://localhost:11434", tt.embeddingModel, tt.chatModel, 60*time.Second)

			if provider.Dimensions() != tt.expectedDim {
				t.Errorf("expected dimensions %d, got %d", tt.expectedDim, provider.Dimensions())
			}

			if provider.ModelID() != tt.embeddingModel {
				t.Errorf("expected model ID '%s', got '%s'", tt.embeddingModel, provider.ModelID())
			}
		})
	}
}

// TestChatMessageStruct tests ChatMessage struct creation.
func TestChatMessageStruct(t *testing.T) {
	msg := ChatMessage{
		Role:    "user",
		Content: "Hello, world!",
	}

	if msg.Role != "user" {
		t.Errorf("expected role 'user', got '%s'", msg.Role)
	}

	if msg.Content != "Hello, world!" {
		t.Errorf("expected content 'Hello, world!', got '%s'", msg.Content)
	}
}

// TestOllamaEmbedLongText tests Embed with long text.
func TestOllamaEmbedLongText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ollamaEmbedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}

		// Verify long text was received
		if len(req.Prompt) < 1000 {
			t.Errorf("expected long prompt, got %d chars", len(req.Prompt))
		}

		resp := ollamaEmbedResponse{
			Embedding: make([]float64, 768),
		}
		for i := range resp.Embedding {
			resp.Embedding[i] = float64(i) / 768.0
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewOllamaProvider(server.URL, "nomic-embed-text", "llama3.1:8b", 60*time.Second)

	// Create a long text
	longText := ""
	for i := 0; i < 100; i++ {
		longText += "This is a test sentence number " + string(rune('0'+i%10)) + ". "
	}

	embedding, err := provider.Embed(context.Background(), longText)
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}

	if len(embedding) != 768 {
		t.Errorf("expected 768 dimensions, got %d", len(embedding))
	}
}

// TestOllamaProviderTimeout tests that timeout is set correctly.
func TestOllamaProviderTimeout(t *testing.T) {
	timeout := 30 * time.Second
	provider := NewOllamaProvider("http://localhost:11434", "nomic-embed-text", "llama3.1:8b", timeout)

	if provider.client.Timeout != timeout {
		t.Errorf("expected timeout %v, got %v", timeout, provider.client.Timeout)
	}
}

// TestOllamaProviderBaseURL tests that base URL is set correctly.
func TestOllamaProviderBaseURL(t *testing.T) {
	baseURL := "http://custom-host:11434"
	provider := NewOllamaProvider(baseURL, "nomic-embed-text", "llama3.1:8b", 60*time.Second)

	if provider.baseURL != baseURL {
		t.Errorf("expected base URL '%s', got '%s'", baseURL, provider.baseURL)
	}
}

// TestOpenAIProviderEmbed tests OpenAI Embed with mock server.
func TestOpenAIProviderEmbed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Errorf("expected path /v1/embeddings, got %s", r.URL.Path)
		}

		if r.Method != "POST" {
			t.Errorf("expected POST method, got %s", r.Method)
		}

		// Check authorization header
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-api-key" {
			t.Errorf("expected Authorization 'Bearer test-api-key', got '%s'", auth)
		}

		var req openAIEmbedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}

		if req.Model != "text-embedding-3-small" {
			t.Errorf("expected model 'text-embedding-3-small', got '%s'", req.Model)
		}

		resp := openAIEmbedResponse{
			Data: []struct {
				Embedding []float64 `json:"embedding"`
			}{
				{Embedding: []float64{0.1, 0.2, 0.3, 0.4, 0.5}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := newOpenAIProviderWithConfig("test-api-key", "text-embedding-3-small", "gpt-4o-mini", server.URL+"/v1", 60*time.Second)

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

// TestOpenAIProviderEmbedError tests OpenAI Embed error handling.
func TestOpenAIProviderEmbedError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := openAIEmbedResponse{
			Error: &struct {
				Message string `json:"message"`
			}{
				Message: "Rate limit exceeded",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := newOpenAIProviderWithConfig("test-api-key", "text-embedding-3-small", "gpt-4o-mini", server.URL+"/v1", 60*time.Second)

	_, err := provider.Embed(context.Background(), "test text")
	if err == nil {
		t.Error("expected error, got nil")
	}
	if err != nil && !contains(err.Error(), "Rate limit exceeded") {
		t.Errorf("expected error about rate limit, got: %v", err)
	}
}

// TestOpenAIProviderEmbedEmptyResponse tests OpenAI Embed with empty data.
func TestOpenAIProviderEmbedEmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := openAIEmbedResponse{
			Data: []struct {
				Embedding []float64 `json:"embedding"`
			}{},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := newOpenAIProviderWithConfig("test-api-key", "text-embedding-3-small", "gpt-4o-mini", server.URL+"/v1", 60*time.Second)

	_, err := provider.Embed(context.Background(), "test text")
	if err == nil {
		t.Error("expected error for empty data, got nil")
	}
}

// TestOpenAIProviderEmbedInvalidJSON tests OpenAI Embed with invalid JSON.
func TestOpenAIProviderEmbedInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not valid json"))
	}))
	defer server.Close()

	provider := newOpenAIProviderWithConfig("test-api-key", "text-embedding-3-small", "gpt-4o-mini", server.URL+"/v1", 60*time.Second)

	_, err := provider.Embed(context.Background(), "test text")
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

// TestOpenAIProviderChat tests OpenAI Chat with mock server.
func TestOpenAIProviderChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("expected path /v1/chat/completions, got %s", r.URL.Path)
		}

		var req openAIChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}

		if req.Model != "gpt-4o-mini" {
			t.Errorf("expected model 'gpt-4o-mini', got '%s'", req.Model)
		}

		resp := openAIChatResponse{
			Choices: []struct {
				Message ChatMessage `json:"message"`
			}{
				{Message: ChatMessage{Role: "assistant", Content: "Hello from OpenAI!"}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := newOpenAIProviderWithConfig("test-api-key", "text-embedding-3-small", "gpt-4o-mini", server.URL+"/v1", 60*time.Second)

	messages := []ChatMessage{
		{Role: "user", Content: "Hello"},
	}

	response, err := provider.Chat(context.Background(), messages)
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	if response != "Hello from OpenAI!" {
		t.Errorf("expected 'Hello from OpenAI!', got '%s'", response)
	}
}

// TestOpenAIProviderChatError tests OpenAI Chat error handling.
func TestOpenAIProviderChatError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := openAIChatResponse{
			Error: &struct {
				Message string `json:"message"`
			}{
				Message: "Invalid API key",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := newOpenAIProviderWithConfig("test-api-key", "text-embedding-3-small", "gpt-4o-mini", server.URL+"/v1", 60*time.Second)

	messages := []ChatMessage{
		{Role: "user", Content: "Hello"},
	}

	_, err := provider.Chat(context.Background(), messages)
	if err == nil {
		t.Error("expected error, got nil")
	}
}

// TestOpenAIProviderChatEmptyChoices tests OpenAI Chat with empty choices.
func TestOpenAIProviderChatEmptyChoices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := openAIChatResponse{
			Choices: []struct {
				Message ChatMessage `json:"message"`
			}{},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := newOpenAIProviderWithConfig("test-api-key", "text-embedding-3-small", "gpt-4o-mini", server.URL+"/v1", 60*time.Second)

	messages := []ChatMessage{
		{Role: "user", Content: "Hello"},
	}

	_, err := provider.Chat(context.Background(), messages)
	if err == nil {
		t.Error("expected error for empty choices, got nil")
	}
}

// TestOpenAIProviderChatInvalidJSON tests OpenAI Chat with invalid JSON.
func TestOpenAIProviderChatInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not valid json"))
	}))
	defer server.Close()

	provider := newOpenAIProviderWithConfig("test-api-key", "text-embedding-3-small", "gpt-4o-mini", server.URL+"/v1", 60*time.Second)

	messages := []ChatMessage{
		{Role: "user", Content: "Hello"},
	}

	_, err := provider.Chat(context.Background(), messages)
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

// TestOpenAIProviderModelID tests OpenAI ModelID.
func TestOpenAIProviderModelID(t *testing.T) {
	provider := newOpenAIProviderWithConfig("test-api-key", "text-embedding-3-small", "gpt-4o-mini", "http://localhost", 60*time.Second)

	if provider.ModelID() != "text-embedding-3-small" {
		t.Errorf("expected 'text-embedding-3-small', got '%s'", provider.ModelID())
	}
}

// TestOpenAIProviderDimensions tests OpenAI Dimensions for different models.
func TestOpenAIProviderDimensionsAllModels(t *testing.T) {
	tests := []struct {
		model      string
		dimensions int
	}{
		{"text-embedding-3-small", 1536},
		{"text-embedding-3-large", 3072},
		{"text-embedding-ada-002", 1536},
		{"custom-model", 1536}, // default
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			provider := newOpenAIProviderWithConfig("test-api-key", tt.model, "gpt-4o-mini", "http://localhost", 60*time.Second)
			if provider.Dimensions() != tt.dimensions {
				t.Errorf("expected %d dimensions, got %d", tt.dimensions, provider.Dimensions())
			}
		})
	}
}

// TestOpenAIProviderContextCancelled tests context cancellation.
func TestOpenAIProviderContextCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	provider := newOpenAIProviderWithConfig("test-api-key", "text-embedding-3-small", "gpt-4o-mini", server.URL+"/v1", 60*time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := provider.Embed(ctx, "test text")
	if err == nil {
		t.Error("expected error with cancelled context")
	}
}

// Helper function for string contains check
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
