package index

import (
	"math"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/ai/template"
)

func TestNewIndex(t *testing.T) {
	idx := NewIndex()
	if idx.Size() != 0 {
		t.Errorf("expected size 0, got %d", idx.Size())
	}
}

func TestIndexAddAndSize(t *testing.T) {
	idx := NewIndex()

	doc := &Document{
		TemplateInfo: &template.TemplateInfo{
			FileName: "test.yml",
		},
		Embedding: []float32{0.1, 0.2, 0.3},
	}

	idx.Add(doc)
	if idx.Size() != 1 {
		t.Errorf("expected size 1, got %d", idx.Size())
	}

	idx.Add(doc)
	if idx.Size() != 2 {
		t.Errorf("expected size 2, got %d", idx.Size())
	}
}

func TestIndexClear(t *testing.T) {
	idx := NewIndex()

	doc := &Document{
		TemplateInfo: &template.TemplateInfo{FileName: "test.yml"},
		Embedding:    []float32{0.1, 0.2, 0.3},
	}

	idx.Add(doc)
	idx.Add(doc)

	idx.Clear()
	if idx.Size() != 0 {
		t.Errorf("expected size 0 after clear, got %d", idx.Size())
	}
}

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name     string
		a        []float32
		b        []float32
		expected float64
		delta    float64
	}{
		{
			name:     "identical vectors",
			a:        []float32{1, 0, 0},
			b:        []float32{1, 0, 0},
			expected: 1.0,
			delta:    0.001,
		},
		{
			name:     "orthogonal vectors",
			a:        []float32{1, 0, 0},
			b:        []float32{0, 1, 0},
			expected: 0.0,
			delta:    0.001,
		},
		{
			name:     "opposite vectors",
			a:        []float32{1, 0, 0},
			b:        []float32{-1, 0, 0},
			expected: -1.0,
			delta:    0.001,
		},
		{
			name:     "similar vectors",
			a:        []float32{1, 1, 0},
			b:        []float32{1, 0, 0},
			expected: 1.0 / math.Sqrt(2),
			delta:    0.001,
		},
		{
			name:     "empty vectors",
			a:        []float32{},
			b:        []float32{},
			expected: 0.0,
			delta:    0.001,
		},
		{
			name:     "different length vectors",
			a:        []float32{1, 0},
			b:        []float32{1, 0, 0},
			expected: 0.0,
			delta:    0.001,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cosineSimilarity(tt.a, tt.b)
			if math.Abs(result-tt.expected) > tt.delta {
				t.Errorf("cosineSimilarity(%v, %v) = %f, expected %f", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}

func TestCalculateKeywordScore(t *testing.T) {
	tmpl := &template.TemplateInfo{
		FileName:     "elxr12-x86_64-edge-raw.yml",
		Distribution: "elxr12",
		ImageType:    "raw",
		Architecture: "x86_64",
		Metadata: template.Metadata{
			Keywords: []string{"edge", "minimal", "iot"},
		},
	}

	tests := []struct {
		name        string
		queryTokens []string
		expectedMin float64
		expectedMax float64
	}{
		{
			name:        "matching keywords",
			queryTokens: []string{"edge", "iot"},
			expectedMin: 0.5,
			expectedMax: 1.0,
		},
		{
			name:        "no matching keywords",
			queryTokens: []string{"cloud", "desktop"},
			expectedMin: 0.0,
			expectedMax: 0.0,
		},
		{
			name:        "partial match",
			queryTokens: []string{"edge", "cloud"},
			expectedMin: 0.25,
			expectedMax: 0.75,
		},
		{
			name:        "distribution match",
			queryTokens: []string{"elxr12"},
			expectedMin: 0.9,
			expectedMax: 1.0,
		},
		{
			name:        "empty query",
			queryTokens: []string{},
			expectedMin: 0.0,
			expectedMax: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := calculateKeywordScore(tt.queryTokens, tmpl)
			if score < tt.expectedMin || score > tt.expectedMax {
				t.Errorf("calculateKeywordScore(%v) = %f, expected between %f and %f",
					tt.queryTokens, score, tt.expectedMin, tt.expectedMax)
			}
		})
	}
}

func TestCalculatePackageScore(t *testing.T) {
	tmpl := &template.TemplateInfo{
		Packages: []string{"nginx", "docker-ce", "openssh-server", "curl"},
	}

	tests := []struct {
		name          string
		queryPackages []string
		expectedMin   float64
		expectedMax   float64
	}{
		{
			name:          "all matching packages",
			queryPackages: []string{"nginx", "docker-ce"},
			expectedMin:   0.9,
			expectedMax:   1.0,
		},
		{
			name:          "partial match with prefix",
			queryPackages: []string{"docker", "nginx"},
			expectedMin:   0.5, // docker matches docker-ce via prefix
			expectedMax:   1.0,
		},
		{
			name:          "no matching packages",
			queryPackages: []string{"apache", "mysql"},
			expectedMin:   0.0,
			expectedMax:   0.0,
		},
		{
			name:          "empty query",
			queryPackages: []string{},
			expectedMin:   0.0,
			expectedMax:   0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := calculatePackageScore(tt.queryPackages, tmpl)
			if score < tt.expectedMin || score > tt.expectedMax {
				t.Errorf("calculatePackageScore(%v) = %f, expected between %f and %f",
					tt.queryPackages, score, tt.expectedMin, tt.expectedMax)
			}
		})
	}
}

func TestCalculateNegationPenalty(t *testing.T) {
	tmpl := &template.TemplateInfo{
		Packages: []string{"docker-ce", "nginx"},
		Metadata: template.Metadata{
			Keywords: []string{"container", "web"},
		},
	}

	tests := []struct {
		name          string
		negativeTerms []string
		penalty       float64
		expected      float64
	}{
		{
			name:          "no negative terms",
			negativeTerms: []string{},
			penalty:       0.5,
			expected:      1.0,
		},
		{
			name:          "negative term in packages",
			negativeTerms: []string{"docker"},
			penalty:       0.5,
			expected:      0.5,
		},
		{
			name:          "negative term in keywords",
			negativeTerms: []string{"container"},
			penalty:       0.5,
			expected:      0.5,
		},
		{
			name:          "no matching negative terms",
			negativeTerms: []string{"mysql", "postgres"},
			penalty:       0.5,
			expected:      1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculateNegationPenalty(tt.negativeTerms, tmpl, tt.penalty)
			if result != tt.expected {
				t.Errorf("calculateNegationPenalty(%v) = %f, expected %f",
					tt.negativeTerms, result, tt.expected)
			}
		})
	}
}

func TestSearch(t *testing.T) {
	idx := NewIndex()

	// Add documents with different embeddings
	doc1 := &Document{
		TemplateInfo: &template.TemplateInfo{
			FileName:     "cloud-template.yml",
			Distribution: "elxr12",
			ImageType:    "raw",
			Architecture: "x86_64",
			Packages:     []string{"cloud-init", "docker-ce"},
			Metadata: template.Metadata{
				Keywords: []string{"cloud", "aws", "azure"},
			},
		},
		Embedding: []float32{0.9, 0.1, 0.0}, // Similar to query
	}

	doc2 := &Document{
		TemplateInfo: &template.TemplateInfo{
			FileName:     "edge-template.yml",
			Distribution: "emt3",
			ImageType:    "raw",
			Architecture: "x86_64",
			Packages:     []string{"kernel", "systemd"},
			Metadata: template.Metadata{
				Keywords: []string{"edge", "iot", "minimal"},
			},
		},
		Embedding: []float32{0.1, 0.9, 0.0}, // Less similar to query
	}

	idx.Add(doc1)
	idx.Add(doc2)

	// Query embedding similar to doc1
	queryEmbedding := []float32{0.95, 0.05, 0.0}

	opts := DefaultSearchOptions()
	opts.MinScore = 0.0 // Accept all results for testing

	results := idx.Search(queryEmbedding, []string{"cloud"}, nil, opts)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// First result should be doc1 (higher semantic similarity + keyword match)
	if results[0].Document.TemplateInfo.FileName != "cloud-template.yml" {
		t.Errorf("expected first result to be cloud-template.yml, got %s",
			results[0].Document.TemplateInfo.FileName)
	}

	// Score should be higher for first result
	if results[0].Score <= results[1].Score {
		t.Errorf("expected first result to have higher score: %f <= %f",
			results[0].Score, results[1].Score)
	}
}

func TestSearchWithNegation(t *testing.T) {
	idx := NewIndex()

	doc1 := &Document{
		TemplateInfo: &template.TemplateInfo{
			FileName: "with-docker.yml",
			Packages: []string{"docker-ce", "nginx"},
		},
		Embedding: []float32{0.9, 0.1, 0.0},
	}

	doc2 := &Document{
		TemplateInfo: &template.TemplateInfo{
			FileName: "without-docker.yml",
			Packages: []string{"podman", "nginx"},
		},
		Embedding: []float32{0.85, 0.15, 0.0},
	}

	idx.Add(doc1)
	idx.Add(doc2)

	queryEmbedding := []float32{0.9, 0.1, 0.0}

	opts := DefaultSearchOptions()
	opts.MinScore = 0.0
	opts.NegativeTerms = []string{"docker"}
	opts.NegationPenalty = 0.5

	results := idx.Search(queryEmbedding, nil, nil, opts)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// First result should be doc2 (without docker) due to negation penalty
	if results[0].Document.TemplateInfo.FileName != "without-docker.yml" {
		t.Errorf("expected first result to be without-docker.yml, got %s",
			results[0].Document.TemplateInfo.FileName)
	}
}

func TestSearchMinScoreThreshold(t *testing.T) {
	idx := NewIndex()

	doc := &Document{
		TemplateInfo: &template.TemplateInfo{
			FileName: "low-match.yml",
		},
		Embedding: []float32{0.1, 0.9, 0.0}, // Low similarity to query
	}

	idx.Add(doc)

	queryEmbedding := []float32{0.9, 0.1, 0.0}

	opts := DefaultSearchOptions()
	opts.MinScore = 0.8 // High threshold

	results := idx.Search(queryEmbedding, nil, nil, opts)

	if len(results) != 0 {
		t.Errorf("expected 0 results with high threshold, got %d", len(results))
	}
}

func TestSearchTopK(t *testing.T) {
	idx := NewIndex()

	// Add 10 documents
	for i := 0; i < 10; i++ {
		doc := &Document{
			TemplateInfo: &template.TemplateInfo{
				FileName: "template.yml",
			},
			Embedding: []float32{float32(i) / 10, 0.5, 0.5},
		}
		idx.Add(doc)
	}

	queryEmbedding := []float32{1.0, 0.5, 0.5}

	opts := DefaultSearchOptions()
	opts.MinScore = 0.0
	opts.TopK = 3

	results := idx.Search(queryEmbedding, nil, nil, opts)

	if len(results) != 3 {
		t.Errorf("expected 3 results with TopK=3, got %d", len(results))
	}
}

func TestGetDocuments(t *testing.T) {
	idx := NewIndex()

	doc1 := &Document{
		TemplateInfo: &template.TemplateInfo{FileName: "doc1.yml"},
		Embedding:    []float32{0.1, 0.2},
	}
	doc2 := &Document{
		TemplateInfo: &template.TemplateInfo{FileName: "doc2.yml"},
		Embedding:    []float32{0.3, 0.4},
	}

	idx.Add(doc1)
	idx.Add(doc2)

	docs := idx.GetDocuments()

	if len(docs) != 2 {
		t.Errorf("expected 2 documents, got %d", len(docs))
	}

	// Verify it's a copy
	docs[0] = nil
	originalDocs := idx.GetDocuments()
	if originalDocs[0] == nil {
		t.Error("GetDocuments should return a copy")
	}
}
