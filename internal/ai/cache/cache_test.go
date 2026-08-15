package cache

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewCache(t *testing.T) {
	tmpDir := t.TempDir()

	cache, err := NewCache(tmpDir)
	if err != nil {
		t.Fatalf("NewCache failed: %v", err)
	}

	// Verify directories were created
	vectorsDir := filepath.Join(tmpDir, "embeddings", "vectors")
	if _, err := os.Stat(vectorsDir); os.IsNotExist(err) {
		t.Error("vectors directory was not created")
	}

	// Verify initial state
	stats := cache.Stats()
	if stats.EntryCount != 0 {
		t.Errorf("expected 0 entries, got %d", stats.EntryCount)
	}
}

func TestComputeContentHash(t *testing.T) {
	content := []byte("test content for hashing")
	hash := ComputeContentHash(content)

	if len(hash) != 16 {
		t.Errorf("expected hash length 16, got %d", len(hash))
	}

	// Same content should produce same hash
	hash2 := ComputeContentHash(content)
	if hash != hash2 {
		t.Error("same content produced different hashes")
	}

	// Different content should produce different hash
	hash3 := ComputeContentHash([]byte("different content"))
	if hash == hash3 {
		t.Error("different content produced same hash")
	}
}

func TestCachePutAndGet(t *testing.T) {
	tmpDir := t.TempDir()
	cache, err := NewCache(tmpDir)
	if err != nil {
		t.Fatalf("NewCache failed: %v", err)
	}

	contentHash := "abcd1234efgh5678"
	modelID := "nomic-embed-text"
	templateName := "test-template.yml"
	dimensions := 768
	embedding := []float32{0.1, 0.2, 0.3, 0.4, 0.5}

	// Put embedding
	if err := cache.Put(PutRequest{
		ContentHash: contentHash,
		ModelID:     modelID,
		Source:      templateName,
		Kind:        "template",
		Dimensions:  dimensions,
		Embedding:   embedding,
	}); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Get embedding
	retrieved, ok := cache.Get(contentHash, modelID)
	if !ok {
		t.Fatal("Get returned false, expected true")
	}

	if len(retrieved) != len(embedding) {
		t.Fatalf("expected %d dimensions, got %d", len(embedding), len(retrieved))
	}

	for i := range embedding {
		if retrieved[i] != embedding[i] {
			t.Errorf("embedding[%d] = %f, expected %f", i, retrieved[i], embedding[i])
		}
	}

	// Verify stats
	stats := cache.Stats()
	if stats.EntryCount != 1 {
		t.Errorf("expected 1 entry, got %d", stats.EntryCount)
	}
	if stats.ModelID != modelID {
		t.Errorf("expected model ID %s, got %s", modelID, stats.ModelID)
	}
	if stats.Dimensions != dimensions {
		t.Errorf("expected dimensions %d, got %d", dimensions, stats.Dimensions)
	}
}

func TestCacheMissForDifferentModel(t *testing.T) {
	tmpDir := t.TempDir()
	cache, err := NewCache(tmpDir)
	if err != nil {
		t.Fatalf("NewCache failed: %v", err)
	}

	contentHash := "abcd1234efgh5678"
	embedding := []float32{0.1, 0.2, 0.3}

	// Put with one model
	if err := cache.Put(PutRequest{
		ContentHash: contentHash,
		ModelID:     "nomic-embed-text",
		Source:      "test.yml",
		Kind:        "template",
		Dimensions:  768,
		Embedding:   embedding,
	}); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Get with different model should miss
	_, ok := cache.Get(contentHash, "different-model")
	if ok {
		t.Error("expected cache miss for different model")
	}
}

func TestCacheMissForNonexistent(t *testing.T) {
	tmpDir := t.TempDir()
	cache, err := NewCache(tmpDir)
	if err != nil {
		t.Fatalf("NewCache failed: %v", err)
	}

	_, ok := cache.Get("nonexistent-hash", "nomic-embed-text")
	if ok {
		t.Error("expected cache miss for nonexistent hash")
	}
}

func TestCacheClear(t *testing.T) {
	tmpDir := t.TempDir()
	cache, err := NewCache(tmpDir)
	if err != nil {
		t.Fatalf("NewCache failed: %v", err)
	}

	// Add some entries
	embedding := []float32{0.1, 0.2, 0.3}
	if err := cache.Put(PutRequest{
		ContentHash: "hash1",
		ModelID:     "model",
		Source:      "t1.yml",
		Kind:        "template",
		Dimensions:  3,
		Embedding:   embedding,
	}); err != nil {
		t.Fatalf("Put failed: %v", err)
	}
	if err := cache.Put(PutRequest{
		ContentHash: "hash2",
		ModelID:     "model",
		Source:      "t2.yml",
		Kind:        "template",
		Dimensions:  3,
		Embedding:   embedding,
	}); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Verify entries exist
	stats := cache.Stats()
	if stats.EntryCount != 2 {
		t.Errorf("expected 2 entries, got %d", stats.EntryCount)
	}

	// Clear cache
	if err := cache.Clear(); err != nil {
		t.Fatalf("Clear failed: %v", err)
	}

	// Verify cache is empty
	stats = cache.Stats()
	if stats.EntryCount != 0 {
		t.Errorf("expected 0 entries after clear, got %d", stats.EntryCount)
	}

	// Verify get returns miss
	_, ok := cache.Get("hash1", "model")
	if ok {
		t.Error("expected cache miss after clear")
	}
}

func TestCacheModelChange(t *testing.T) {
	tmpDir := t.TempDir()
	cache, err := NewCache(tmpDir)
	if err != nil {
		t.Fatalf("NewCache failed: %v", err)
	}

	embedding1 := []float32{0.1, 0.2, 0.3}
	embedding2 := []float32{0.4, 0.5, 0.6, 0.7}

	// Put with first model
	if err := cache.Put(PutRequest{
		ContentHash: "hash1",
		ModelID:     "model1",
		Source:      "t1.yml",
		Kind:        "template",
		Dimensions:  3,
		Embedding:   embedding1,
	}); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Put with different model should clear cache
	if err := cache.Put(PutRequest{
		ContentHash: "hash2",
		ModelID:     "model2",
		Source:      "t2.yml",
		Kind:        "template",
		Dimensions:  4,
		Embedding:   embedding2,
	}); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// First entry should be gone
	_, ok := cache.Get("hash1", "model2")
	if ok {
		t.Error("expected first entry to be cleared after model change")
	}

	// Second entry should exist
	retrieved, ok := cache.Get("hash2", "model2")
	if !ok {
		t.Error("expected second entry to exist")
	}

	if len(retrieved) != len(embedding2) {
		t.Errorf("expected %d dimensions, got %d", len(embedding2), len(retrieved))
	}
}

func TestCachePersistence(t *testing.T) {
	tmpDir := t.TempDir()

	// Create cache and add entry
	cache1, err := NewCache(tmpDir)
	if err != nil {
		t.Fatalf("NewCache failed: %v", err)
	}

	embedding := []float32{0.1, 0.2, 0.3, 0.4, 0.5}
	if err := cache1.Put(PutRequest{
		ContentHash: "persistent-hash",
		ModelID:     "model",
		Source:      "test.yml",
		Kind:        "template",
		Dimensions:  5,
		Embedding:   embedding,
	}); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Create new cache instance and verify entry persists
	cache2, err := NewCache(tmpDir)
	if err != nil {
		t.Fatalf("NewCache failed: %v", err)
	}

	retrieved, ok := cache2.Get("persistent-hash", "model")
	if !ok {
		t.Fatal("expected entry to persist across cache instances")
	}

	if len(retrieved) != len(embedding) {
		t.Fatalf("expected %d dimensions, got %d", len(embedding), len(retrieved))
	}

	for i := range embedding {
		if retrieved[i] != embedding[i] {
			t.Errorf("embedding[%d] = %f, expected %f", i, retrieved[i], embedding[i])
		}
	}
}

func TestCachePutAndGetBundleAndPackage(t *testing.T) {
	tmpDir := t.TempDir()
	cache, err := NewCache(tmpDir)
	if err != nil {
		t.Fatalf("NewCache failed: %v", err)
	}

	bundleReq := PutRequest{
		ContentHash: "bundlehash123456",
		ModelID:     "nomic-embed-text",
		Source:      "realsense-camera",
		Kind:        "bundle",
		Dimensions:  3,
		Embedding:   []float32{0.5, 0.6, 0.7},
	}
	if err := cache.Put(bundleReq); err != nil {
		t.Fatalf("Put bundle failed: %v", err)
	}

	pkgReq := PutRequest{
		ContentHash: "pkghash123456789",
		ModelID:     "nomic-embed-text",
		Source:      "librealsense2",
		Kind:        "package",
		Dimensions:  3,
		Embedding:   []float32{0.8, 0.9, 1.0},
	}
	if err := cache.Put(pkgReq); err != nil {
		t.Fatalf("Put package failed: %v", err)
	}

	// Retrieve bundle
	bundleVec, ok := cache.Get("bundlehash123456", "nomic-embed-text")
	if !ok {
		t.Fatal("expected bundle to be retrieved")
	}
	if len(bundleVec) != 3 || bundleVec[0] != 0.5 {
		t.Errorf("unexpected bundle vec: %v", bundleVec)
	}

	// Retrieve package
	pkgVec, ok := cache.Get("pkghash123456789", "nomic-embed-text")
	if !ok {
		t.Fatal("expected package to be retrieved")
	}
	if len(pkgVec) != 3 || pkgVec[0] != 0.8 {
		t.Errorf("unexpected package vec: %v", pkgVec)
	}
}

func TestCacheOldFormatMissesOnce(t *testing.T) {
	tmpDir := t.TempDir()

	// Write a fake old-format index.json where the key was "template"
	oldIndexJSON := `{
  "model_id": "nomic-embed-text",
  "dimensions": 3,
  "created_at": "2026-01-01T00:00:00Z",
  "entries": {
    "oldhash12345678": {
      "template": "legacy-template.yml",
      "content_hash": "oldhash12345678",
      "updated_at": "2026-01-01T00:00:00Z"
    }
  }
}`
	embeddingsDir := filepath.Join(tmpDir, "embeddings")
	vectorsDir := filepath.Join(embeddingsDir, "vectors")
	if err := os.MkdirAll(vectorsDir, 0755); err != nil {
		t.Fatalf("failed to create vectors dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(embeddingsDir, "index.json"), []byte(oldIndexJSON), 0644); err != nil {
		t.Fatalf("failed to write old index.json: %v", err)
	}

	// Save vector file
	vectorPath := filepath.Join(vectorsDir, "oldhash12345678.bin")
	if err := saveEmbedding(vectorPath, []float32{0.1, 0.2, 0.3}); err != nil {
		t.Fatalf("failed to save vector: %v", err)
	}

	// Open cache
	cache, err := NewCache(tmpDir)
	if err != nil {
		t.Fatalf("NewCache failed: %v", err)
	}

	// Calling Get should miss once without crashing (because Source is empty)
	_, ok := cache.Get("oldhash12345678", "nomic-embed-text")
	if ok {
		t.Error("expected old format cache entry to miss once")
	}

	// Subsequent Put with new PutRequest updates it
	if err := cache.Put(PutRequest{
		ContentHash: "oldhash12345678",
		ModelID:     "nomic-embed-text",
		Source:      "legacy-template.yml",
		Kind:        "template",
		Dimensions:  3,
		Embedding:   []float32{0.1, 0.2, 0.3},
	}); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Now Get should hit
	retrieved, ok := cache.Get("oldhash12345678", "nomic-embed-text")
	if !ok {
		t.Fatal("expected entry to hit after Put")
	}
	if len(retrieved) != 3 {
		t.Errorf("expected 3 dimensions, got %d", len(retrieved))
	}
}

func TestLoadSaveEmbedding(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.bin")

	original := []float32{0.123, -0.456, 0.789, 1.0, -1.0, 0.0}

	if err := saveEmbedding(path, original); err != nil {
		t.Fatalf("saveEmbedding failed: %v", err)
	}

	loaded, err := loadEmbedding(path)
	if err != nil {
		t.Fatalf("loadEmbedding failed: %v", err)
	}

	if len(loaded) != len(original) {
		t.Fatalf("expected %d dimensions, got %d", len(original), len(loaded))
	}

	for i := range original {
		if loaded[i] != original[i] {
			t.Errorf("embedding[%d] = %f, expected %f", i, loaded[i], original[i])
		}
	}
}
