// Package provider provides embedding and chat APIs for different AI providers.
package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// ErrProviderUnavailable indicates the AI provider could not be reached
// (connection refused, DNS failure, TLS error, timeout, etc.). Callers can
// detect it with errors.Is to map to a PROVIDER_UNAVAILABLE API response,
// rather than matching on error-message substrings.
var ErrProviderUnavailable = errors.New("AI provider unavailable")

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

// StreamingChatProvider extends chat capabilities with token-by-token streaming.
// This is a separate interface (not added to ChatProvider) to avoid breaking
// existing implementations. Use a type assertion to check support:
//
//	if sp, ok := chatProvider.(StreamingChatProvider); ok { ... }
type StreamingChatProvider interface {
	// ChatStream opens a streaming chat session. It returns a channel that
	// delivers individual tokens, an error channel for mid-stream failures,
	// and an immediate error if the connection cannot be established.
	// The tokens channel is always closed when generation completes.
	ChatStream(ctx context.Context, messages []ChatMessage) (tokens <-chan string, errc <-chan error, err error)
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
		return nil, fmt.Errorf("failed to call Ollama API: %w: %v", ErrProviderUnavailable, err)
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
		return "", fmt.Errorf("failed to call Ollama API: %w: %v", ErrProviderUnavailable, err)
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

// ollamaStreamChunk represents a single chunk in Ollama's NDJSON stream.
type ollamaStreamChunk struct {
	Model   string `json:"model"`
	Message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"message"`
	Done bool `json:"done"`
}

// ChatStream opens a streaming chat session with Ollama.
// Ollama streams NDJSON (one JSON object per line) with stream: true.
func (p *OllamaProvider) ChatStream(ctx context.Context, messages []ChatMessage) (<-chan string, <-chan error, error) {
	reqBody := ollamaChatRequest{
		Model:    p.chatModel,
		Messages: messages,
		Stream:   true,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Derive a cancelable context so an idle-stream watchdog can abort a
	// hung generation even while the client stays connected.
	streamCtx, cancel := context.WithCancel(ctx)

	req, err := http.NewRequestWithContext(streamCtx, "POST", p.baseURL+"/api/chat", bytes.NewReader(jsonBody))
	if err != nil {
		cancel()
		return nil, nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Use a client without an overall timeout: an HTTP-client Timeout would
	// abort a long-but-healthy stream. The idle watchdog below plus context
	// cancellation handle cleanup instead.
	streamClient := &http.Client{}
	resp, err := streamClient.Do(req)
	if err != nil {
		cancel()
		return nil, nil, fmt.Errorf("failed to call Ollama API: %w: %v", ErrProviderUnavailable, err)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		cancel()
		return nil, nil, fmt.Errorf("Ollama API error (status %d): %s", resp.StatusCode, string(body))
	}

	tokens := make(chan string)
	errc := make(chan error, 1)

	go func() {
		defer cancel()
		defer close(tokens)
		defer resp.Body.Close()

		// Idle watchdog: if no token arrives within the provider's configured
		// timeout, cancel the context to abort the blocked read. This bounds
		// the goroutine lifetime when the LLM backend hangs.
		idle := time.AfterFunc(p.streamIdleTimeout(), cancel)
		defer idle.Stop()

		scanner := bufio.NewScanner(resp.Body)
		// Increase buffer for safety (default 64KB is usually fine for chat).
		scanner.Buffer(make([]byte, 0, 256*1024), 256*1024)

		for scanner.Scan() {
			idle.Reset(p.streamIdleTimeout())

			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}

			var chunk ollamaStreamChunk
			if err := json.Unmarshal(line, &chunk); err != nil {
				errc <- fmt.Errorf("failed to parse Ollama stream chunk: %w", err)
				return
			}

			if chunk.Message.Content != "" {
				select {
				case tokens <- chunk.Message.Content:
				case <-streamCtx.Done():
					errc <- streamCtx.Err()
					return
				}
			}

			if chunk.Done {
				return
			}
		}

		if err := scanner.Err(); err != nil {
			errc <- fmt.Errorf("Ollama stream read error: %w", err)
		}
	}()

	return tokens, errc, nil
}

// streamIdleTimeout returns the maximum time a stream may go without
// producing a token before it is aborted. It reuses the provider's configured
// request timeout, falling back to a sane default if unset.
func (p *OllamaProvider) streamIdleTimeout() time.Duration {
	if p.client != nil && p.client.Timeout > 0 {
		return p.client.Timeout
	}
	return 120 * time.Second
}

// DefaultOpenAIBaseURL is the default base URL for OpenAI API.
const DefaultOpenAIBaseURL = "https://api.openai.com"

// OpenAIProvider implements EmbeddingProvider and ChatProvider using OpenAI API.
type OpenAIProvider struct {
	apiKey         string
	embeddingModel string
	chatModel      string
	client         *http.Client
	dimensions     int
	baseURL        string
}

// NewOpenAIProvider creates a new OpenAI provider.
func NewOpenAIProvider(embeddingModel, chatModel string, timeout time.Duration) (*OpenAIProvider, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY environment variable is not set")
	}

	return newOpenAIProviderWithConfig(apiKey, embeddingModel, chatModel, DefaultOpenAIBaseURL, timeout), nil
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

	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/v1/embeddings", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call OpenAI API: %w: %v", ErrProviderUnavailable, err)
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

	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/v1/chat/completions", bytes.NewReader(jsonBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to call OpenAI API: %w: %v", ErrProviderUnavailable, err)
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

// openAIChatStreamRequest adds the stream flag for OpenAI streaming requests.
type openAIChatStreamRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

// openAIStreamChunk represents a single chunk in OpenAI's SSE stream.
type openAIStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// ChatStream opens a streaming chat session with OpenAI.
// OpenAI streams SSE with "data: {json}" lines, ending with "data: [DONE]".
func (p *OpenAIProvider) ChatStream(ctx context.Context, messages []ChatMessage) (<-chan string, <-chan error, error) {
	reqBody := openAIChatStreamRequest{
		Model:    p.chatModel,
		Messages: messages,
		Stream:   true,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Derive a cancelable context so an idle-stream watchdog can abort a
	// hung generation even while the client stays connected.
	streamCtx, cancel := context.WithCancel(ctx)

	req, err := http.NewRequestWithContext(streamCtx, "POST", p.baseURL+"/v1/chat/completions", bytes.NewReader(jsonBody))
	if err != nil {
		cancel()
		return nil, nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	// Use a client without an overall timeout: an HTTP-client Timeout would
	// abort a long-but-healthy stream. The idle watchdog below plus context
	// cancellation handle cleanup instead.
	streamClient := &http.Client{}
	resp, err := streamClient.Do(req)
	if err != nil {
		cancel()
		return nil, nil, fmt.Errorf("failed to call OpenAI API: %w: %v", ErrProviderUnavailable, err)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		cancel()
		return nil, nil, fmt.Errorf("OpenAI API error (status %d): %s", resp.StatusCode, string(body))
	}

	tokens := make(chan string)
	errc := make(chan error, 1)

	go func() {
		defer cancel()
		defer close(tokens)
		defer resp.Body.Close()

		// Idle watchdog: if no data arrives within the provider's configured
		// timeout, cancel the context to abort the blocked read. This bounds
		// the goroutine lifetime when the LLM backend hangs.
		idle := time.AfterFunc(p.streamIdleTimeout(), cancel)
		defer idle.Stop()

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 256*1024), 256*1024)

		for scanner.Scan() {
			idle.Reset(p.streamIdleTimeout())

			line := scanner.Text()

			// Skip empty lines and non-data lines (SSE format).
			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			data := strings.TrimPrefix(line, "data: ")

			// OpenAI signals end of stream with [DONE].
			if data == "[DONE]" {
				return
			}

			var chunk openAIStreamChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				errc <- fmt.Errorf("failed to parse OpenAI stream chunk: %w", err)
				return
			}

			if chunk.Error != nil {
				errc <- fmt.Errorf("OpenAI API error: %s", chunk.Error.Message)
				return
			}

			if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
				select {
				case tokens <- chunk.Choices[0].Delta.Content:
				case <-streamCtx.Done():
					errc <- streamCtx.Err()
					return
				}
			}
		}

		if err := scanner.Err(); err != nil {
			errc <- fmt.Errorf("OpenAI stream read error: %w", err)
		}
	}()

	return tokens, errc, nil
}

// streamIdleTimeout returns the maximum time a stream may go without
// producing data before it is aborted. It reuses the provider's configured
// request timeout, falling back to a sane default if unset.
func (p *OpenAIProvider) streamIdleTimeout() time.Duration {
	if p.client != nil && p.client.Timeout > 0 {
		return p.client.Timeout
	}
	return 120 * time.Second
}
