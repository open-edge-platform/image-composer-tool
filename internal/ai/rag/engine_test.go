package rag

import (
	"strings"
	"testing"
)

func TestParseQuery(t *testing.T) {
	tests := []struct {
		name             string
		query            string
		expectedTokens   []string
		expectedPackages []string
		expectedNegative []string
	}{
		{
			name:             "simple query",
			query:            "create cloud image",
			expectedTokens:   []string{"cloud", "image"},
			expectedPackages: nil,
			expectedNegative: nil,
		},
		{
			name:             "query with packages",
			query:            "image with nginx and docker",
			expectedTokens:   []string{"image", "nginx", "docker"},
			expectedPackages: []string{"nginx", "docker"},
			expectedNegative: nil,
		},
		{
			name:             "query with negation",
			query:            "minimal edge image without docker",
			expectedTokens:   []string{"minimal", "edge", "image"},
			expectedPackages: nil,
			expectedNegative: []string{"docker"},
		},
		{
			name:             "query with exclude",
			query:            "cloud image exclude mysql",
			expectedTokens:   []string{"cloud", "image"},
			expectedPackages: nil,
			expectedNegative: []string{"mysql"},
		},
		{
			name:             "complex query",
			query:            "I want to create an edge IoT image with nginx but without docker",
			expectedTokens:   []string{"edge", "iot", "image", "nginx"},
			expectedPackages: []string{"nginx"},
			expectedNegative: []string{"docker"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens, packages, negatives := parseQuery(tt.query)

			// Check tokens (allowing for different order)
			for _, expected := range tt.expectedTokens {
				found := false
				for _, token := range tokens {
					if token == expected {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected token '%s' not found in %v", expected, tokens)
				}
			}

			// Check packages
			for _, expected := range tt.expectedPackages {
				found := false
				for _, pkg := range packages {
					if strings.Contains(pkg, expected) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected package '%s' not found in %v", expected, packages)
				}
			}

			// Check negative terms
			for _, expected := range tt.expectedNegative {
				found := false
				for _, neg := range negatives {
					if neg == expected {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected negative term '%s' not found in %v", expected, negatives)
				}
			}
		})
	}
}

func TestIsStopWord(t *testing.T) {
	stopWords := []string{"a", "the", "is", "are", "and", "or", "for", "to", "with"}
	nonStopWords := []string{"cloud", "edge", "docker", "minimal", "image"}

	for _, word := range stopWords {
		if !isStopWord(word) {
			t.Errorf("expected '%s' to be a stop word", word)
		}
	}

	for _, word := range nonStopWords {
		if isStopWord(word) {
			t.Errorf("expected '%s' to not be a stop word", word)
		}
	}
}

func TestCleanYAMLResponse(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "with code block",
			input:    "Here is the template:\n```yaml\nimage:\n  name: test\n```\nDone!",
			expected: "Here is the template:\nimage:\n  name: test\nDone!",
		},
		{
			name:     "with yml code block",
			input:    "```yml\nimage:\n  name: test\n```",
			expected: "image:\n  name: test",
		},
		{
			name:     "no code block",
			input:    "image:\n  name: test",
			expected: "image:\n  name: test",
		},
		{
			name:     "multiple code blocks",
			input:    "Example:\n```yaml\nfirst: value\n```\nAnother:\n```yaml\nsecond: value\n```",
			expected: "Example:\nfirst: value\nAnother:\nsecond: value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CleanYAMLResponse(tt.input)
			if result != tt.expected {
				t.Errorf("CleanYAMLResponse() = %q, expected %q", result, tt.expected)
			}
		})
	}
}
