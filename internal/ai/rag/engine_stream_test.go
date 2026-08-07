package rag

import (
	"context"
	"errors"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/ai/index"
	"github.com/open-edge-platform/image-composer-tool/internal/ai/provider"
	"github.com/open-edge-platform/image-composer-tool/internal/ai/template"
)

// mockEmbedProvider returns a fixed embedding so search is deterministic.
type mockEmbedProvider struct{}

func (m *mockEmbedProvider) Embed(_ context.Context, _ string) ([]float32, error) {
	return []float32{1, 0, 0}, nil
}
func (m *mockEmbedProvider) ModelID() string { return "mock-embed" }
func (m *mockEmbedProvider) Dimensions() int { return 3 }

// mockStreamingChatProvider implements both ChatProvider and
// StreamingChatProvider. The behavior is driven by the configured fields.
type mockStreamingChatProvider struct {
	chatResp  string
	chatErr   error
	tokens    []string
	streamErr error // sent on errc mid-stream
	startErr  error // returned immediately from ChatStream
}

func (m *mockStreamingChatProvider) Chat(_ context.Context, _ []provider.ChatMessage) (string, error) {
	if m.chatErr != nil {
		return "", m.chatErr
	}
	return m.chatResp, nil
}
func (m *mockStreamingChatProvider) ModelID() string { return "mock-chat" }

func (m *mockStreamingChatProvider) ChatStream(ctx context.Context, _ []provider.ChatMessage) (<-chan string, <-chan error, error) {
	if m.startErr != nil {
		return nil, nil, m.startErr
	}
	tokens := make(chan string)
	errc := make(chan error, 1)
	go func() {
		defer close(tokens)
		for _, tok := range m.tokens {
			select {
			case tokens <- tok:
			case <-ctx.Done():
				errc <- ctx.Err()
				return
			}
		}
		if m.streamErr != nil {
			errc <- m.streamErr
		}
	}()
	return tokens, errc, nil
}

// nonStreamingChatProvider implements ChatProvider only.
type nonStreamingChatProvider struct{}

func (nonStreamingChatProvider) Chat(_ context.Context, _ []provider.ChatMessage) (string, error) {
	return "yaml", nil
}
func (nonStreamingChatProvider) ModelID() string { return "mock-nostream" }

// newTestEngine builds an initialized engine with one indexed template and
// the given chat provider injected.
func newTestEngine(chat provider.ChatProvider) *Engine {
	e := &Engine{
		embedProvider: &mockEmbedProvider{},
		chatProvider:  chat,
		index:         index.NewIndex(),
		initialized:   true,
	}
	e.index.Add(&index.Document{
		TemplateInfo: &template.TemplateInfo{
			FileName:   "example.yml",
			RawContent: []byte("image:\n  name: example\n"),
		},
		Embedding:      []float32{1, 0, 0},
		SearchableText: "example edge image",
	})
	return e
}

func TestGenerateStreamHappyPath(t *testing.T) {
	chat := &mockStreamingChatProvider{tokens: []string{"image:\n", "  name: x\n"}}
	e := newTestEngine(chat)

	result, err := e.GenerateStream(context.Background(), "edge image")
	if err != nil {
		t.Fatalf("GenerateStream returned error: %v", err)
	}

	if len(result.SearchResults) == 0 {
		t.Error("expected search results in stream result")
	}
	if len(result.SourceTemplates) == 0 || result.SourceTemplates[0] != "example.yml" {
		t.Errorf("expected source template example.yml, got %v", result.SourceTemplates)
	}

	var got string
	for tok := range result.TokenChan {
		got += tok
	}
	if got != "image:\n  name: x\n" {
		t.Errorf("assembled tokens mismatch, got %q", got)
	}

	// Non-blocking error check after channel close.
	select {
	case err := <-result.ErrChan:
		if err != nil {
			t.Errorf("unexpected stream error: %v", err)
		}
	default:
	}
}

func TestGenerateStreamMidStreamError(t *testing.T) {
	wantErr := errors.New("llm exploded")
	chat := &mockStreamingChatProvider{tokens: []string{"partial"}, streamErr: wantErr}
	e := newTestEngine(chat)

	result, err := e.GenerateStream(context.Background(), "edge image")
	if err != nil {
		t.Fatalf("GenerateStream returned error: %v", err)
	}

	for range result.TokenChan { // drain
	}

	select {
	case gotErr := <-result.ErrChan:
		if !errors.Is(gotErr, wantErr) {
			t.Errorf("expected stream error %v, got %v", wantErr, gotErr)
		}
	default:
		t.Error("expected an error on ErrChan after mid-stream failure")
	}
}

func TestGenerateStreamProviderDoesNotSupportStreaming(t *testing.T) {
	e := newTestEngine(nonStreamingChatProvider{})

	_, err := e.GenerateStream(context.Background(), "edge image")
	if err == nil {
		t.Fatal("expected error when provider does not support streaming")
	}
}

func TestGenerateWithContextPopulatesSources(t *testing.T) {
	chat := &mockStreamingChatProvider{chatResp: "```yaml\nimage:\n  name: x\n```"}
	e := newTestEngine(chat)

	result, err := e.GenerateWithContext(context.Background(), "edge image")
	if err != nil {
		t.Fatalf("GenerateWithContext returned error: %v", err)
	}

	if result.YAML != "image:\n  name: x" {
		t.Errorf("expected cleaned YAML, got %q", result.YAML)
	}
	if len(result.SourceTemplates) == 0 || result.SourceTemplates[0] != "example.yml" {
		t.Errorf("expected source template example.yml, got %v", result.SourceTemplates)
	}
	if len(result.SearchResults) == 0 {
		t.Error("expected search results")
	}
}
