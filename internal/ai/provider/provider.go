// Package provider provides embedding and chat APIs for different AI providers.
package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// EmbeddingProvider defines the interface for generating embeddings.
type EmbeddingProvider interface {
	// Embed generates an embedding vector for the given text.
	Embed(ctx context.Context, text string) ([]float32, error)

	// ModelID returns the identifier of the embedding model being used.
	ModelID() string

	// Dimensions returns the expected embedding dimensions for this provider.
	Dimensions() int
}

// ChatProvider defines the interface for chat completions.
type ChatProvider interface {
	// Chat sends a message and returns the response.
	Chat(ctx context.Context, messages []ChatMessage) (string, error)

	// ModelID returns the identifier of the chat model being used.
	ModelID() string
}

// ChatMessage represents a message in a chat conversation.
type ChatMessage struct {
	Role    string `json:"role"`    // "system", "user", or "assistant"
	Content string `json:"content"` // Message content
}

// OllamaProvider implements EmbeddingProvider and ChatProvider using Ollama.
type OllamaProvider struct {
	baseURL        string
	embeddingModel string
	chatModel      string
	client         *http.Client
	dimensions     int
}

// NewOllamaProvider creates a new Ollama provider.
func NewOllamaProvider(baseURL, embeddingModel, chatModel string, timeout time.Duration) *OllamaProvider {
	// Determine dimensions based on model
	dimensions := 768 // default for nomic-embed-text
	switch embeddingModel {
	case "nomic-embed-text":
		dimensions = 768
	case "mxbai-embed-large":
		dimensions = 1024
	case "all-minilm":
		dimensions = 384
	}

	return &OllamaProvider{
		baseURL:        baseURL,
		embeddingModel: embeddingModel,
		chatModel:      chatModel,
		client: &http.Client{
			Timeout: timeout,
		},
		dimensions: dimensions,
	}
}

// ollamaEmbedRequest is the request body for Ollama embeddings API.
type ollamaEmbedRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

// ollamaEmbedResponse is the response body from Ollama embeddings API.
type ollamaEmbedResponse struct {
	Embedding []float64 `json:"embedding"`
}

// Embed generates an embedding using Ollama.
func (p *OllamaProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	reqBody := ollamaEmbedRequest{
		Model:  p.embeddingModel,
		Prompt: text,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/api/embeddings", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call Ollama API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Ollama API error (status %d): %s", resp.StatusCode, string(body))
	}

	var embedResp ollamaEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&embedResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Convert float64 to float32
	embedding := make([]float32, len(embedResp.Embedding))
	for i, v := range embedResp.Embedding {
		embedding[i] = float32(v)
	}

	return embedding, nil
}

// ModelID returns the embedding model identifier.
func (p *OllamaProvider) ModelID() string {
	return p.embeddingModel
}

// Dimensions returns the embedding dimensions.
func (p *OllamaProvider) Dimensions() int {
	return p.dimensions
}

// ollamaChatRequest is the request body for Ollama chat API.
type ollamaChatRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

// ollamaChatResponse is the response body from Ollama chat API.
type ollamaChatResponse struct {
	Message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"message"`
}

// Chat sends a chat request to Ollama.
func (p *OllamaProvider) Chat(ctx context.Context, messages []ChatMessage) (string, error) {
	reqBody := ollamaChatRequest{
		Model:    p.chatModel,
		Messages: messages,
		Stream:   false,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/api/chat", bytes.NewReader(jsonBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to call Ollama API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("Ollama API error (status %d): %s", resp.StatusCode, string(body))
	}

	var chatResp ollamaChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	return chatResp.Message.Content, nil
}

// DefaultOpenAIBaseURL is the default base URL for OpenAI API. Like the
// OpenAI SDKs' OPENAI_BASE_URL, it includes the /v1 prefix, so an
// OpenAI-compatible service such as GNAI is set by its published URL.
const DefaultOpenAIBaseURL = "https://api.openai.com/v1"

// OpenAIProvider implements EmbeddingProvider and ChatProvider using OpenAI API.
type OpenAIProvider struct {
	apiKey string
	// apiKeyFile, when set, holds the key instead of apiKey. It is read on
	// every request, so a short-lived token (such as a GNAI SSO token) can be
	// replaced without restarting.
	apiKeyFile     string
	embeddingModel string
	chatModel      string
	client         *http.Client
	dimensions     int
	baseURL        string
}

// NewOpenAIProvider creates a new OpenAI provider. baseURL is the API root
// including /v1. The OPENAI_BASE_URL environment variable overrides it, the
// way OPENAI_API_KEY supplies the key; if both are empty it is
// DefaultOpenAIBaseURL. OPENAI_MODEL likewise overrides chatModel.
//
// The key is OPENAI_API_KEY, or else the contents of the file
// OPENAI_API_KEY_FILE names, read again for every request.
func NewOpenAIProvider(baseURL, embeddingModel, chatModel string, timeout time.Duration) (*OpenAIProvider, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	keyFile := ""
	if apiKey == "" {
		keyFile = os.Getenv("OPENAI_API_KEY_FILE")
		if keyFile == "" {
			return nil, fmt.Errorf("OPENAI_API_KEY environment variable is not set")
		}
		if _, err := readKeyFile(keyFile); err != nil {
			return nil, err
		}
	}
	if env := os.Getenv("OPENAI_BASE_URL"); env != "" {
		baseURL = env
	}
	if env := os.Getenv("OPENAI_MODEL"); env != "" {
		chatModel = env
	}
	if baseURL == "" {
		baseURL = DefaultOpenAIBaseURL
	}

	p := newOpenAIProviderWithConfig(apiKey, embeddingModel, chatModel, strings.TrimRight(baseURL, "/"), timeout)
	p.apiKeyFile = keyFile
	return p, nil
}

// readKeyFile reads an API key file, trimming the newline an editor or
// `echo` leaves.
func readKeyFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading OPENAI_API_KEY_FILE: %w", err)
	}
	key := strings.TrimSpace(string(data))
	if key == "" {
		return "", fmt.Errorf("OPENAI_API_KEY_FILE %s is empty", path)
	}
	return key, nil
}

// authorize sets the request's bearer token.
func (p *OpenAIProvider) authorize(req *http.Request) error {
	key := p.apiKey
	if p.apiKeyFile != "" {
		var err error
		if key, err = readKeyFile(p.apiKeyFile); err != nil {
			return err
		}
	}
	req.Header.Set("Authorization", "Bearer "+key)
	return nil
}

// newOpenAIProviderWithConfig creates an OpenAI provider with explicit configuration (for testing).
func newOpenAIProviderWithConfig(apiKey, embeddingModel, chatModel, baseURL string, timeout time.Duration) *OpenAIProvider {
	// Determine dimensions based on model
	dimensions := 1536 // default for text-embedding-3-small
	switch embeddingModel {
	case "text-embedding-3-small":
		dimensions = 1536
	case "text-embedding-3-large":
		dimensions = 3072
	case "text-embedding-ada-002":
		dimensions = 1536
	}

	return &OpenAIProvider{
		apiKey:         apiKey,
		embeddingModel: embeddingModel,
		chatModel:      chatModel,
		client: &http.Client{
			Timeout: timeout,
		},
		dimensions: dimensions,
		baseURL:    baseURL,
	}
}

// openAIEmbedRequest is the request body for OpenAI embeddings API.
type openAIEmbedRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

// openAIEmbedResponse is the response body from OpenAI embeddings API.
type openAIEmbedResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Embed generates an embedding using OpenAI.
func (p *OpenAIProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	reqBody := openAIEmbedRequest{
		Model: p.embeddingModel,
		Input: text,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/embeddings", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if err := p.authorize(req); err != nil {
		return nil, err
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call OpenAI API: %w", err)
	}
	defer resp.Body.Close()

	var embedResp openAIEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&embedResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if embedResp.Error != nil {
		return nil, fmt.Errorf("OpenAI API error: %s", embedResp.Error.Message)
	}

	if len(embedResp.Data) == 0 {
		return nil, fmt.Errorf("no embedding returned from OpenAI")
	}

	// Convert float64 to float32
	embedding := make([]float32, len(embedResp.Data[0].Embedding))
	for i, v := range embedResp.Data[0].Embedding {
		embedding[i] = float32(v)
	}

	return embedding, nil
}

// ModelID returns the embedding model identifier.
func (p *OpenAIProvider) ModelID() string {
	return p.embeddingModel
}

// BaseURL returns the API root the provider calls, including /v1.
func (p *OpenAIProvider) BaseURL() string {
	return p.baseURL
}

// Dimensions returns the embedding dimensions.
func (p *OpenAIProvider) Dimensions() int {
	return p.dimensions
}

// openAIChatRequest is the request body for OpenAI chat API.
type openAIChatRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
}

// openAIChatResponse is the response body from OpenAI chat API.
type openAIChatResponse struct {
	Choices []struct {
		Message ChatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Chat sends a chat request to OpenAI.
func (p *OpenAIProvider) Chat(ctx context.Context, messages []ChatMessage) (string, error) {
	reqBody := openAIChatRequest{
		Model:    p.chatModel,
		Messages: messages,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/chat/completions", bytes.NewReader(jsonBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if err := p.authorize(req); err != nil {
		return "", err
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to call OpenAI API: %w", err)
	}
	defer resp.Body.Close()

	var chatResp openAIChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if chatResp.Error != nil {
		return "", fmt.Errorf("OpenAI API error: %s", chatResp.Error.Message)
	}

	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("no response from OpenAI")
	}

	return chatResp.Choices[0].Message.Content, nil
}

// APIError is an error status from an OpenAI-compatible service.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("OpenAI API error (HTTP %d): %s", e.StatusCode, e.Message)
}

// Unauthorized reports whether the service rejected the API key, as it does
// once a short-lived token expires.
func (e *APIError) Unauthorized() bool {
	return e.StatusCode == http.StatusUnauthorized || e.StatusCode == http.StatusForbidden
}

// ChatJSON sends a chat request whose answer must be JSON matching schema
// (response_format json_schema, strict), and returns that JSON. name labels
// the schema for the service.
func (p *OpenAIProvider) ChatJSON(ctx context.Context, messages []ChatMessage, name string, schema map[string]any) (string, error) {
	reqBody := map[string]any{
		"model":    p.chatModel,
		"messages": messages,
		"response_format": map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   name,
				"strict": true,
				"schema": schema,
			},
		},
	}
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/chat/completions", bytes.NewReader(jsonBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if err := p.authorize(req); err != nil {
		return "", err
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to call OpenAI API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}
	var chatResp openAIChatResponse
	decodeErr := json.Unmarshal(body, &chatResp)
	if resp.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(body))
		if decodeErr == nil && chatResp.Error != nil {
			msg = chatResp.Error.Message
		}
		if len(msg) > 500 {
			msg = msg[:500]
		}
		return "", &APIError{StatusCode: resp.StatusCode, Message: msg}
	}
	if decodeErr != nil {
		return "", fmt.Errorf("failed to decode response: %w", decodeErr)
	}
	if chatResp.Error != nil {
		return "", &APIError{StatusCode: resp.StatusCode, Message: chatResp.Error.Message}
	}
	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("no response from OpenAI")
	}
	return chatResp.Choices[0].Message.Content, nil
}
